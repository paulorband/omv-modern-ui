package server

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"omvhome/internal/apps"
	"omvhome/internal/auth"
	"omvhome/internal/catalog"
	"omvhome/internal/compose"
	"omvhome/internal/docker"
	"omvhome/internal/omv"
	"omvhome/internal/store"
	"omvhome/internal/system"
)

func newStoreServer(t *testing.T) (http.Handler, *http.Cookie) {
	t.Helper()
	dm := docker.NewMock()
	cr := compose.NewMockRunner(dm)
	root := t.TempDir()
	appSvc := &apps.Service{Docker: dm, Compose: cr, DataDir: filepath.Join(root, "data"), AppDataDir: filepath.Join(root, "appdata")}
	cat, err := catalog.New("")
	if err != nil {
		t.Fatal(err)
	}
	inst := store.NewInstaller(cat, appSvc, dm, cr, "1000", "100", "UTC")
	inst.PortBusy = func(int) bool { return false }
	inst.MkdirAll = func(string) error { return nil }
	sim := system.NewSimCollector()
	h := New(Deps{
		Auth: auth.NewService(omv.NewMock()), Collector: sim, Hub: system.NewHub(sim, 0),
		Docker: dm, Compose: cr, Apps: appSvc, Catalog: cat, Store: inst,
	})
	return h, login(t, h)
}

func TestStoreRequiresAuth(t *testing.T) {
	h, _ := newStoreServer(t)
	for _, p := range []string{"/api/v1/store/apps", "/api/v1/store/apps/jellyfin", "/api/v1/store/installs/x/events"} {
		if rec := do(h, "GET", p, "", nil, false); rec.Code != 401 {
			t.Errorf("%s: %d", p, rec.Code)
		}
	}
}

func TestStoreListAndDetail(t *testing.T) {
	h, c := newStoreServer(t)
	rec := do(h, "GET", "/api/v1/store/apps?q=jelly", "", c, false)
	var cards []struct {
		ID        string `json:"id"`
		Installed bool   `json:"installed"`
	}
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &cards) != nil || len(cards) != 1 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if !cards[0].Installed { // the Docker mock seeds a jellyfin app
		t.Error("jellyfin should be flagged installed")
	}
	rec = do(h, "GET", "/api/v1/store/apps/nextcloud", "", c, false)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "NEXTCLOUD_ADMIN_PASSWORD") || !strings.Contains(rec.Body.String(), `"installed":false`) {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "GET", "/api/v1/store/apps/nope", "", c, false); rec.Code != 404 {
		t.Fatalf("unknown: %d", rec.Code)
	}
}

func TestStoreInstallFlowOverHTTP(t *testing.T) {
	h, c := newStoreServer(t)
	rec := do(h, "POST", "/api/v1/store/apps/uptime-kuma/install", `{"port":3055}`, c, true)
	if rec.Code != 202 || !strings.Contains(rec.Body.String(), `"jobId":"uptime-kuma"`) {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	rec = do(h, "GET", "/api/v1/store/installs/uptime-kuma/events", "", c, false)
	body := rec.Body.String()
	if rec.Code != 200 || !strings.Contains(body, "event: starting") || !strings.Contains(body, "event: done") {
		t.Fatalf("events: %d\n%s", rec.Code, body)
	}

	rec = do(h, "GET", "/api/v1/apps", "", c, false)
	if !strings.Contains(rec.Body.String(), `"id":"uptime-kuma"`) || !strings.Contains(rec.Body.String(), `"port":3055`) {
		t.Fatalf("app not listed: %s", rec.Body)
	}
	if rec := do(h, "POST", "/api/v1/store/apps/uptime-kuma/install", `{}`, c, true); rec.Code != 409 {
		t.Fatalf("second install: %d", rec.Code)
	}
}

func TestStoreInstallErrorsOverHTTP(t *testing.T) {
	h, c := newStoreServer(t)
	cases := []struct {
		path, body string
		want       int
	}{
		{"/api/v1/store/apps/nope/install", `{}`, 404},
		{"/api/v1/store/apps/nextcloud/install", `{}`, 400},                                    // required password
		{"/api/v1/store/apps/qbittorrent/install", `{"env":{"DOWNLOADS_DIR":"/a/../b"}}`, 400}, // traversal
		{"/api/v1/store/apps/uptime-kuma/install", `{"port":8096}`, 409},                       // port used by jellyfin
		{"/api/v1/store/apps/uptime-kuma/install", `not json`, 400},
		{"/api/v1/store/custom", `{"name":"x","compose":"services:\n  a:\n    image: i\n    privileged: true\n"}`, 400},
	}
	for _, tc := range cases {
		if rec := do(h, "POST", tc.path, tc.body, c, true); rec.Code != tc.want {
			t.Errorf("%s %s: got %d want %d (%s)", tc.path, tc.body, rec.Code, tc.want, rec.Body)
		}
	}
	if rec := do(h, "GET", "/api/v1/store/installs/never/events", "", c, false); rec.Code != 404 {
		t.Errorf("events of unknown job: %d", rec.Code)
	}
}

func TestStoreCustomInstallOverHTTP(t *testing.T) {
	h, c := newStoreServer(t)
	body := `{"name":"Whoami","compose":"services:\n  whoami:\n    image: traefik/whoami\n    ports: [\"8099:80\"]\n","port":8099}`
	rec := do(h, "POST", "/api/v1/store/custom", body, c, true)
	if rec.Code != 202 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	_ = do(h, "GET", "/api/v1/store/installs/whoami/events", "", c, false)
	if rec := do(h, "GET", "/api/v1/apps", "", c, false); !strings.Contains(rec.Body.String(), `"id":"whoami"`) {
		t.Fatalf("custom app missing: %s", rec.Body)
	}
}

func TestStoreInstallsListAndDismissOverHTTP(t *testing.T) { // A10.1-A10.3
	h, c := newStoreServer(t)
	if rec := do(h, "GET", "/api/v1/store/installs", "", nil, false); rec.Code != 401 {
		t.Fatalf("list without session: %d", rec.Code)
	}
	if rec := do(h, "GET", "/api/v1/store/installs", "", c, false); rec.Code != 200 || strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatalf("empty list: %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "POST", "/api/v1/store/apps/uptime-kuma/install", `{"port":3055}`, c, true); rec.Code != 202 {
		t.Fatalf("install: %d %s", rec.Code, rec.Body)
	}
	_ = do(h, "GET", "/api/v1/store/installs/uptime-kuma/events", "", c, false) // waits for the job

	rec := do(h, "GET", "/api/v1/store/installs", "", c, false)
	var list []struct {
		ID, Title, State string
	}
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &list) != nil || len(list) != 1 || list[0].ID != "uptime-kuma" || list[0].Title != "Uptime Kuma" || list[0].State != "done" {
		t.Fatalf("list: %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "DELETE", "/api/v1/store/installs/uptime-kuma", "", c, false); rec.Code != 403 {
		t.Errorf("dismiss without CSRF header: %d", rec.Code)
	}
	if rec := do(h, "DELETE", "/api/v1/store/installs/uptime-kuma", "", c, true); rec.Code != 204 {
		t.Fatalf("dismiss: %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "DELETE", "/api/v1/store/installs/uptime-kuma", "", c, true); rec.Code != 404 {
		t.Errorf("dismiss twice: %d", rec.Code)
	}
	if rec := do(h, "GET", "/api/v1/store/installs", "", c, false); strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Errorf("list after dismiss: %s", rec.Body)
	}
}
