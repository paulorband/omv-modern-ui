package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"omvhome/internal/auth"
	"omvhome/internal/docker"
	"omvhome/internal/omv"
	"omvhome/internal/storage"
	"omvhome/internal/system"
)

const testFS = "11111111-aaaa-4aaa-8aaa-111111111111"
const seededShare = "aaaaaaaa-0000-4000-8000-000000000001"

func newStorageServer(t *testing.T) (http.Handler, *http.Cookie, *omv.Mock) {
	t.Helper()
	m := omv.NewMock()
	omv.RegisterStorageFixtures(m)
	omv.RegisterShareFixtures(m)
	omv.ApplyPollInterval = 1
	sim := system.NewSimCollector()
	h := New(Deps{
		Auth: auth.NewService(m), Collector: sim, Hub: system.NewHub(sim, 0),
		Docker: docker.NewMock(), Storage: &storage.Service{},
	})
	return h, login(t, h), m
}

func TestStorageRequiresAuth(t *testing.T) {
	h, _, _ := newStorageServer(t)
	for _, p := range []string{"/api/v1/storage/filesystems", "/api/v1/storage/shares", "/api/v1/storage/users"} {
		if rec := do(h, "GET", p, "", nil, false); rec.Code != 401 {
			t.Errorf("%s: %d", p, rec.Code)
		}
	}
}

func TestStorageReadEndpoints(t *testing.T) {
	h, c, _ := newStorageServer(t)
	var fs []omv.Filesystem
	rec := do(h, "GET", "/api/v1/storage/filesystems", "", c, false)
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &fs) != nil || len(fs) != 2 || fs[0].UUID != testFS {
		t.Fatalf("filesystems: %d %s", rec.Code, rec.Body)
	}
	var shares []storage.Share
	rec = do(h, "GET", "/api/v1/storage/shares", "", c, false)
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &shares) != nil || len(shares) != 1 || !shares[0].SMB {
		t.Fatalf("shares: %d %s", rec.Code, rec.Body)
	}
	rec = do(h, "GET", "/api/v1/storage/users", "", c, false)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"name":"admin"`) {
		t.Fatalf("users: %d %s", rec.Code, rec.Body)
	}
}

func TestStorageShareLifecycleOverHTTP(t *testing.T) {
	h, c, _ := newStorageServer(t)
	body := `{"name":"docs","filesystemUuid":"` + testFS + `","relativePath":"docs","smb":true,"comment":"Meus docs"}`
	rec := do(h, "POST", "/api/v1/storage/shares", body, c, true)
	var sh storage.Share
	if rec.Code != 201 || json.Unmarshal(rec.Body.Bytes(), &sh) != nil || sh.Name != "docs" || !sh.SMB || sh.UUID == "" {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}

	rec = do(h, "PATCH", "/api/v1/storage/shares/"+sh.UUID, `{"smbGuest":true}`, c, true)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"smbGuest":true`) {
		t.Fatalf("patch: %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "DELETE", "/api/v1/storage/shares/"+sh.UUID, "", c, true); rec.Code != 204 {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "DELETE", "/api/v1/storage/shares/"+sh.UUID, "", c, true); rec.Code != 404 {
		t.Fatalf("second delete: %d", rec.Code)
	}
}

func TestStorageErrorMapping(t *testing.T) {
	h, c, m := newStorageServer(t)
	cases := []struct {
		name, method, path, body string
		want                     int
	}{
		{"invalid name", "POST", "/api/v1/storage/shares", `{"name":"bad name","filesystemUuid":"` + testFS + `"}`, 400},
		{"traversal", "POST", "/api/v1/storage/shares", `{"name":"x","filesystemUuid":"` + testFS + `","relativePath":"../etc"}`, 400},
		{"duplicate", "POST", "/api/v1/storage/shares", `{"name":"media","filesystemUuid":"` + testFS + `"}`, 409},
		{"bad json", "POST", "/api/v1/storage/shares", `{`, 400},
		{"bad uuid", "DELETE", "/api/v1/storage/shares/not-a-uuid", ``, 400},
		{"patch unknown", "PATCH", "/api/v1/storage/shares/aaaaaaaa-0000-4000-8000-0000000000ff", `{"smb":true}`, 404},
		{"csrf", "DELETE", "/api/v1/storage/shares/" + seededShare, ``, 403},
	}
	for _, tc := range cases {
		xhr := tc.name != "csrf"
		if rec := do(h, tc.method, tc.path, tc.body, c, xhr); rec.Code != tc.want {
			t.Errorf("%s: got %d want %d (%s)", tc.name, rec.Code, tc.want, rec.Body)
		}
	}

	// OMV failing mid-way surfaces as 502 naming the step, and the share is rolled back (A06.2).
	m.Handle("SMB.setShare", func(json.RawMessage) (any, error) { return nil, &omv.RPCError{Code: 1, Message: "smb broken"} })
	rec := do(h, "POST", "/api/v1/storage/shares", `{"name":"docs","filesystemUuid":"`+testFS+`","smb":true}`, c, true)
	if rec.Code != 502 || !strings.Contains(rec.Body.String(), "SMB") {
		t.Fatalf("step failure: %d %s", rec.Code, rec.Body)
	}
	rec = do(h, "GET", "/api/v1/storage/shares", "", c, false)
	if strings.Contains(rec.Body.String(), `"name":"docs"`) {
		t.Fatalf("share must be rolled back: %s", rec.Body)
	}
}

func TestMetaExposesOMVPort(t *testing.T) {
	m := omv.NewMock()
	h := New(Deps{Auth: auth.NewService(m), OMVPort: 81})
	if rec := do(h, "GET", "/api/v1/meta", "", nil, false); rec.Code != 401 {
		t.Fatalf("meta must require auth: %d", rec.Code)
	}
	c := login(t, h)
	rec := do(h, "GET", "/api/v1/meta", "", c, false)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"omvPort":81`) {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
}
