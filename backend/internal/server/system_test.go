package server

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"omvhome/internal/auth"
	"omvhome/internal/omv"
	"omvhome/internal/system"
)

func newSystemServer(t *testing.T, m *omv.Mock) http.Handler {
	t.Helper()
	sim := system.NewSimCollector()
	return New(Deps{
		Auth:      auth.NewService(m),
		Collector: sim,
		Hub:       system.NewHub(sim, 50*time.Millisecond),
	})
}

func TestSystemRoutesRequireAuth(t *testing.T) {
	h := newSystemServer(t, omv.NewMock())
	for _, p := range []string{"/api/v1/system", "/api/v1/system/stats", "/api/v1/system/disks"} {
		if rec := do(h, "GET", p, "", nil, false); rec.Code != 401 {
			t.Errorf("%s: %d", p, rec.Code)
		}
	}
}

func TestSystemInfoAndStats(t *testing.T) {
	m := omv.NewMock()
	omv.RegisterStorageFixtures(m)
	h := newSystemServer(t, m)
	c := login(t, h)

	rec := do(h, "GET", "/api/v1/system", "", c, false)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"omvVersion":"7.4.17-1 (Sandworm)"`) {
		t.Fatalf("info: %d %s", rec.Code, rec.Body)
	}
	rec = do(h, "GET", "/api/v1/system/stats", "", c, false)
	var st system.Stats
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &st) != nil || st.Memory.Total == 0 {
		t.Fatalf("stats: %d %s", rec.Code, rec.Body)
	}
}

func TestSystemInfoToleratesOMVVersionFailure(t *testing.T) {
	h := newSystemServer(t, omv.NewMock()) // no fixtures: System.getInformation fails
	c := login(t, h)
	rec := do(h, "GET", "/api/v1/system", "", c, false)
	if rec.Code != 200 {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
}

func TestDisksListsOnlyMounted(t *testing.T) {
	m := omv.NewMock()
	omv.RegisterStorageFixtures(m)
	h := newSystemServer(t, m)
	c := login(t, h)
	rec := do(h, "GET", "/api/v1/system/disks", "", c, false)
	var disks []omv.Filesystem
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &disks) != nil {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if len(disks) != 2 {
		t.Fatalf("want 2 mounted filesystems, got %d", len(disks))
	}
	if disks[0].Label != "data" || disks[0].Percent != 38 || disks[0].Total == 0 {
		t.Errorf("disk0: %+v", disks[0])
	}
}

func TestDisksOMVFailureIs502(t *testing.T) { // A02.4
	m := omv.NewMock()
	m.Handle("FileSystemMgmt.enumerateFilesystems", func(json.RawMessage) (any, error) {
		return nil, &omv.RPCError{Code: 100, Message: "boom"}
	})
	h := newSystemServer(t, m)
	c := login(t, h)
	rec := do(h, "GET", "/api/v1/system/disks", "", c, false)
	if rec.Code != 502 || !strings.Contains(rec.Body.String(), "upstream_error") {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
}

func TestStatsStreamEmitsEvents(t *testing.T) { // A02.3
	h := newSystemServer(t, omv.NewMock())
	ts := httptest.NewServer(h)
	defer ts.Close()

	rec := do(h, "POST", "/api/v1/auth/login", `{"username":"admin","password":"admin"}`, nil, true)
	c := sessionCookie(rec)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+"/api/v1/system/stats/stream", nil)
	req.AddCookie(c)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content-type %q", ct)
	}
	events := 0
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() && events < 2 {
		if strings.HasPrefix(sc.Text(), "event: stats") {
			events++
		}
	}
	if events < 2 {
		t.Fatalf("got %d events", events)
	}
}
