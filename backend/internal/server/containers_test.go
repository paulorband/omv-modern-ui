package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"omvhome/internal/auth"
	"omvhome/internal/docker"
	"omvhome/internal/omv"
	"omvhome/internal/system"
)

// spyDocker counts every call that reaches the Docker layer.
type spyDocker struct {
	docker.Client
	calls atomic.Int32
}

func (s *spyDocker) InspectContainer(ctx context.Context, id string) (*docker.Detail, error) {
	s.calls.Add(1)
	return s.Client.InspectContainer(ctx, id)
}
func (s *spyDocker) StartContainer(ctx context.Context, id string) error {
	s.calls.Add(1)
	return s.Client.StartContainer(ctx, id)
}
func (s *spyDocker) RemoveContainer(ctx context.Context, id string, f, v bool) error {
	s.calls.Add(1)
	return s.Client.RemoveContainer(ctx, id, f, v)
}

func newDockerServer(t *testing.T, dc docker.Client) (http.Handler, *http.Cookie) {
	t.Helper()
	sim := system.NewSimCollector()
	h := New(Deps{Auth: auth.NewService(omv.NewMock()), Collector: sim, Hub: system.NewHub(sim, 0), Docker: dc})
	return h, login(t, h)
}

func TestContainersRequireAuth(t *testing.T) {
	h, _ := newDockerServer(t, docker.NewMock())
	for _, p := range []string{"/api/v1/containers", "/api/v1/images", "/api/v1/docker/status"} {
		if rec := do(h, "GET", p, "", nil, false); rec.Code != 401 {
			t.Errorf("%s: %d", p, rec.Code)
		}
	}
}

func TestListAndLifecycle(t *testing.T) {
	h, c := newDockerServer(t, docker.NewMock())
	rec := do(h, "GET", "/api/v1/containers", "", c, false)
	var list []docker.Container
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &list) != nil || len(list) < 4 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "POST", "/api/v1/containers/nginx-test/stop", "", c, true); rec.Code != 204 {
		t.Fatalf("stop: %d %s", rec.Code, rec.Body)
	}
	rec = do(h, "GET", "/api/v1/containers/nginx-test", "", c, false)
	if !strings.Contains(rec.Body.String(), `"state":"exited"`) {
		t.Fatalf("state not updated: %s", rec.Body)
	}
	if rec := do(h, "DELETE", "/api/v1/containers/nginx-test", "", c, true); rec.Code != 204 {
		t.Fatalf("remove plain container: %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "GET", "/api/v1/containers/nginx-test", "", c, false); rec.Code != 404 {
		t.Fatalf("after remove: %d", rec.Code)
	}
}

func TestInvalidIDDoesNotReachDocker(t *testing.T) { // A03.4
	spy := &spyDocker{Client: docker.NewMock()}
	h, c := newDockerServer(t, spy)
	for _, id := range []string{"..%2Fetc", "a%2Fb", "-x", "a%20b"} {
		for _, m := range []string{"GET", "DELETE"} {
			rec := do(h, m, "/api/v1/containers/"+id, "", c, true)
			if rec.Code != 400 {
				t.Errorf("%s %s: %d", m, id, rec.Code)
			}
		}
		if rec := do(h, "POST", "/api/v1/containers/"+id+"/start", "", c, true); rec.Code != 400 {
			t.Errorf("start %s: %d", id, rec.Code)
		}
	}
	if n := spy.calls.Load(); n != 0 {
		t.Fatalf("docker called %d times", n)
	}
}

func TestRemoveAppContainerIsConflict(t *testing.T) { // A03.5
	mock := docker.NewMock()
	h, c := newDockerServer(t, mock)
	rec := do(h, "DELETE", "/api/v1/containers/jellyfin?force=true", "", c, true)
	if rec.Code != 409 || !strings.Contains(rec.Body.String(), "jellyfin") {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if _, err := mock.InspectContainer(context.Background(), "jellyfin"); err != nil {
		t.Fatal("container must still exist")
	}
}

func TestDetailMasksSecrets(t *testing.T) { // A03.6
	h, c := newDockerServer(t, docker.NewMock())
	rec := do(h, "GET", "/api/v1/containers/pihole", "", c, false)
	body := rec.Body.String()
	if rec.Code != 200 || strings.Contains(body, "hunter2") || !strings.Contains(body, "••••••••") {
		t.Fatalf("%d %s", rec.Code, body)
	}
}

func TestImportRunEndpoint(t *testing.T) { // A03.3 (HTTP)
	h, c := newDockerServer(t, docker.NewMock())
	rec := do(h, "POST", "/api/v1/containers/import-run", `{"command":"docker run -d --name x -p 8080:80 img"}`, c, true)
	var spec docker.RunSpec
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &spec) != nil || spec.Name != "x" || spec.Ports[0].Host != 8080 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	rec = do(h, "POST", "/api/v1/containers/import-run", `{"command":"docker run --rm img"}`, c, true)
	if rec.Code != 400 || !strings.Contains(rec.Body.String(), "--rm") {
		t.Fatalf("unknown flag: %d %s", rec.Code, rec.Body)
	}
}

func TestRunEndpoint(t *testing.T) {
	h, c := newDockerServer(t, docker.NewMock())
	rec := do(h, "POST", "/api/v1/containers/run", `{"image":"redis:7","name":"cache","ports":[{"host":6379,"container":6379}]}`, c, true)
	if rec.Code != 201 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "POST", "/api/v1/containers/run", `{"image":"redis:7","name":"../x"}`, c, true); rec.Code != 400 {
		t.Fatalf("invalid name: %d", rec.Code)
	}
	if rec := do(h, "POST", "/api/v1/containers/run", `{"image":"x","labels":{"omvhome.app":"evil"}}`, c, true); rec.Code != 400 {
		t.Fatalf("labels must be rejected: %d", rec.Code)
	}
}

func TestDockerUnavailableIs502(t *testing.T) { // R03.10
	dead := httptest.NewServer(http.NotFoundHandler())
	url := dead.URL
	dead.Close()
	h, c := newDockerServer(t, docker.NewHTTPClient(url))

	rec := do(h, "GET", "/api/v1/containers", "", c, false)
	if rec.Code != 502 || !strings.Contains(rec.Body.String(), "Docker indisponível") {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	rec = do(h, "GET", "/api/v1/docker/status", "", c, false)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"available":false`) {
		t.Fatalf("status: %d %s", rec.Code, rec.Body)
	}
}

func TestLogsEndpoint(t *testing.T) {
	h, c := newDockerServer(t, docker.NewMock())
	rec := do(h, "GET", "/api/v1/containers/jellyfin/logs?tail=2", "", c, false)
	if rec.Code != 200 || strings.Count(rec.Body.String(), "\n") != 2 {
		t.Fatalf("%d %q", rec.Code, rec.Body)
	}
	if rec := do(h, "GET", "/api/v1/containers/jellyfin/logs?tail=abc", "", c, false); rec.Code != 400 {
		t.Fatalf("bad tail: %d", rec.Code)
	}
}

func TestImagesEndpoints(t *testing.T) {
	h, c := newDockerServer(t, docker.NewMock())
	rec := do(h, "GET", "/api/v1/images", "", c, false)
	var imgs []docker.Image
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &imgs) != nil || len(imgs) == 0 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "DELETE", "/api/v1/images/a1b2c3d4e5f6", "", c, true); rec.Code != 409 {
		t.Fatalf("in-use image: %d", rec.Code)
	}
	if rec := do(h, "DELETE", "/api/v1/images/d4e5f6a1b2c3", "", c, true); rec.Code != 204 {
		t.Fatalf("unused image: %d", rec.Code)
	}
	if rec := do(h, "DELETE", "/api/v1/images/nothex", "", c, true); rec.Code != 400 {
		t.Fatalf("bad id: %d", rec.Code)
	}
	rec = do(h, "POST", "/api/v1/images/pull", `{"ref":"alpine:3"}`, c, true)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "event: done") {
		t.Fatalf("pull: %d %s", rec.Code, rec.Body)
	}
}
