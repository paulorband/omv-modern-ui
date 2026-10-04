package server

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"omvhome/internal/apps"
	"omvhome/internal/auth"
	"omvhome/internal/compose"
	"omvhome/internal/docker"
	"omvhome/internal/omv"
	"omvhome/internal/system"
)

func newAppsServer(t *testing.T) (http.Handler, *http.Cookie, *docker.Mock) {
	t.Helper()
	dm := docker.NewMock()
	cr := compose.NewMockRunner(dm)
	root := t.TempDir()
	sim := system.NewSimCollector()
	h := New(Deps{
		Auth: auth.NewService(omv.NewMock()), Collector: sim, Hub: system.NewHub(sim, 0),
		Docker: dm, Compose: cr,
		Apps: &apps.Service{Docker: dm, Compose: cr, DataDir: filepath.Join(root, "data"), AppDataDir: filepath.Join(root, "appdata")},
	})
	return h, login(t, h), dm
}

func TestAppsRequireAuth(t *testing.T) {
	h, _, _ := newAppsServer(t)
	if rec := do(h, "GET", "/api/v1/apps", "", nil, false); rec.Code != 401 {
		t.Fatalf("%d", rec.Code)
	}
}

func TestAppsListAndLifecycle(t *testing.T) { // A04.5 (API side)
	h, c, _ := newAppsServer(t)
	rec := do(h, "GET", "/api/v1/apps", "", c, false)
	var list []apps.App
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &list) != nil || len(list) != 3 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	state := func(id string) string {
		var l []apps.App
		_ = json.Unmarshal(do(h, "GET", "/api/v1/apps", "", c, false).Body.Bytes(), &l)
		for _, a := range l {
			if a.ID == id {
				return a.State
			}
		}
		return "missing"
	}
	if state("jellyfin") != "running" || state("syncthing") != "stopped" {
		t.Fatalf("seed states wrong: %s %s", state("jellyfin"), state("syncthing"))
	}
	if rec := do(h, "POST", "/api/v1/apps/jellyfin/stop", "", c, true); rec.Code != 204 {
		t.Fatalf("stop: %d %s", rec.Code, rec.Body)
	}
	if state("jellyfin") != "stopped" {
		t.Fatal("jellyfin should be stopped")
	}
	if rec := do(h, "POST", "/api/v1/apps/syncthing/start", "", c, true); rec.Code != 204 || state("syncthing") != "running" {
		t.Fatalf("start: %d", rec.Code)
	}
}

func TestAppsErrors(t *testing.T) {
	h, c, _ := newAppsServer(t)
	if rec := do(h, "POST", "/api/v1/apps/ghost/start", "", c, true); rec.Code != 404 {
		t.Errorf("unknown app: %d", rec.Code)
	}
	if rec := do(h, "POST", "/api/v1/apps/Bad_ID/start", "", c, true); rec.Code != 400 {
		t.Errorf("bad id: %d", rec.Code)
	}
	if rec := do(h, "POST", "/api/v1/apps/jellyfin/update", "", c, true); rec.Code != 409 {
		t.Errorf("update unmanaged: %d %s", rec.Code, rec.Body)
	}
}

func TestAppsUninstall(t *testing.T) {
	h, c, dm := newAppsServer(t)
	if rec := do(h, "DELETE", "/api/v1/apps/pihole", "", c, true); rec.Code != 204 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	cs, _ := dm.ListContainers(context.Background())
	for _, x := range cs {
		if x.Name == "pihole" {
			t.Fatal("pihole should be removed")
		}
	}
	if rec := do(h, "DELETE", "/api/v1/apps/pihole", "", c, true); rec.Code != 404 {
		t.Fatalf("second delete: %d", rec.Code)
	}
}

func TestAppLogsStreamAndDockerStatusCompose(t *testing.T) {
	h, c, _ := newAppsServer(t)
	rec := do(h, "GET", "/api/v1/docker/status", "", c, false)
	if !strings.Contains(rec.Body.String(), `"composeAvailable":true`) {
		t.Fatalf("status: %s", rec.Body)
	}
	if rec := do(h, "GET", "/api/v1/apps/ghost/logs/stream", "", c, false); rec.Code != 404 {
		t.Fatalf("logs of unknown app: %d", rec.Code)
	}
}
