package server

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
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

func zipOf(t *testing.T, top string, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, err := zw.Create(top + name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(body))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func umbrelApp(id, extraService string) map[string]string {
	return map[string]string{
		id + "/umbrel-app.yml": "id: " + id + "\nname: " + strings.ToUpper(id) + "\nport: 7000\npath: /ui\n",
		id + "/docker-compose.yml": "services:\n  app_proxy:\n    environment:\n      APP_HOST: " + id + "_web_1\n      APP_PORT: 80\n" +
			"  web:\n    image: acme/web:1\n    environment:\n      - SEED=${APP_SEED}\n    volumes:\n      - ${APP_DATA_DIR}/data:/data\n" + extraService,
	}
}

func repoZip(t *testing.T) []byte {
	files := map[string]string{"umbrel-app-store.yml": "id: acme\nname: Acme Store\n"}
	for k, v := range umbrelApp("good", "") {
		files[k] = v
	}
	for k, v := range umbrelApp("risky", "    privileged: true\n") {
		files[k] = v
	}
	files["needs-dep/umbrel-app.yml"] = "id: needs-dep\nname: Needs Dep\nport: 1\ndependencies:\n  - bitcoin\n"
	files["needs-dep/docker-compose.yml"] = "services:\n  app_proxy:\n    environment:\n      APP_HOST: needs-dep_web_1\n      APP_PORT: 1\n  web:\n    image: x\n"
	return zipOf(t, "acme-main/", files)
}

type sourcesEnv struct {
	h      http.Handler
	c      *http.Cookie
	zipURL string
	apps   *apps.Service
}

func newSourcesEnv(t *testing.T) sourcesEnv {
	t.Helper()
	dm := docker.NewMock()
	cr := compose.NewMockRunner(dm)
	root := t.TempDir()
	appSvc := &apps.Service{Docker: dm, Compose: cr, DataDir: filepath.Join(root, "data"), AppDataDir: filepath.Join(root, "appdata")}
	cat, err := catalog.New("")
	if err != nil {
		t.Fatal(err)
	}
	if err := cat.UseDir(filepath.Join(root, "data")); err != nil {
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
	zipBytes := repoZip(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/acme.zip" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(zipBytes)
	}))
	t.Cleanup(srv.Close)
	return sourcesEnv{h: h, c: login(t, h), zipURL: srv.URL + "/acme.zip", apps: appSvc}
}

func TestSourcesRequireAuthAndCSRF(t *testing.T) {
	e := newSourcesEnv(t)
	if rec := do(e.h, "GET", "/api/v1/store/sources", "", nil, false); rec.Code != 401 {
		t.Errorf("list without session: %d", rec.Code)
	}
	if rec := do(e.h, "POST", "/api/v1/store/sources", `{"url":"`+e.zipURL+`"}`, nil, true); rec.Code != 401 {
		t.Errorf("add without session: %d", rec.Code)
	}
	if rec := do(e.h, "POST", "/api/v1/store/sources", `{"url":"`+e.zipURL+`"}`, e.c, false); rec.Code != 403 {
		t.Errorf("add without CSRF header: %d", rec.Code)
	}
}

func TestSourcesAddListRefreshRemove(t *testing.T) { // R09.1-R09.4, R09.5
	e := newSourcesEnv(t)
	rec := do(e.h, "POST", "/api/v1/store/sources", `{"url":"`+e.zipURL+`"}`, e.c, true)
	var src catalog.Source
	if rec.Code != 201 || json.Unmarshal(rec.Body.Bytes(), &src) != nil || src.ID != "acme" || src.Name != "Acme Store" || src.Format != "umbrel" || src.Apps != 3 {
		t.Fatalf("add: %d %s", rec.Code, rec.Body)
	}
	if rec := do(e.h, "POST", "/api/v1/store/sources", `{"url":"`+e.zipURL+`"}`, e.c, true); rec.Code != 409 {
		t.Errorf("duplicate: %d", rec.Code)
	}
	for _, bad := range []string{`{"url":"ftp://x/y.zip"}`, `{"url":""}`, `{"url":"` + e.zipURL + `-missing.zip"}`} {
		want := 400
		if strings.Contains(bad, "missing") {
			want = 502
		}
		if rec := do(e.h, "POST", "/api/v1/store/sources", bad, e.c, true); rec.Code != want {
			t.Errorf("%s: %d %s", bad, rec.Code, rec.Body)
		}
	}

	rec = do(e.h, "GET", "/api/v1/store/sources", "", e.c, false)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"id":"acme"`) {
		t.Fatalf("list: %d %s", rec.Code, rec.Body)
	}

	// store cards carry source/unsupported and can be filtered
	rec = do(e.h, "GET", "/api/v1/store/apps?source=acme", "", e.c, false)
	var cards []struct {
		ID          string `json:"id"`
		Source      string `json:"source"`
		Unsupported bool   `json:"unsupported"`
	}
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &cards) != nil || len(cards) != 3 {
		t.Fatalf("cards: %d %s", rec.Code, rec.Body)
	}
	for _, c := range cards {
		if c.Source != "acme" || c.Unsupported != (c.ID == "needs-dep") {
			t.Errorf("card %+v", c)
		}
	}
	if rec := do(e.h, "GET", "/api/v1/store/apps?source=builtin", "", e.c, false); strings.Contains(rec.Body.String(), `"acme"`) {
		t.Errorf("builtin filter leaked source apps: %s", rec.Body)
	}

	if rec := do(e.h, "POST", "/api/v1/store/sources/acme/refresh", "", e.c, true); rec.Code != 200 {
		t.Errorf("refresh: %d %s", rec.Code, rec.Body)
	}
	if rec := do(e.h, "POST", "/api/v1/store/sources/nope/refresh", "", e.c, true); rec.Code != 404 {
		t.Errorf("refresh unknown: %d", rec.Code)
	}
	if rec := do(e.h, "DELETE", "/api/v1/store/sources/acme", "", e.c, true); rec.Code != 204 {
		t.Errorf("delete: %d", rec.Code)
	}
	if rec := do(e.h, "DELETE", "/api/v1/store/sources/acme", "", e.c, true); rec.Code != 404 {
		t.Errorf("delete twice: %d", rec.Code)
	}
	if rec := do(e.h, "GET", "/api/v1/store/apps/good", "", e.c, false); rec.Code != 404 {
		t.Errorf("app should vanish with its source: %d", rec.Code)
	}
}

func TestSourceAppDetailAndInstall(t *testing.T) { // R09.5, R09.6, A09.4
	e := newSourcesEnv(t)
	if rec := do(e.h, "POST", "/api/v1/store/sources", `{"url":"`+e.zipURL+`"}`, e.c, true); rec.Code != 201 {
		t.Fatalf("add: %d %s", rec.Code, rec.Body)
	}

	var detail struct {
		Source      string   `json:"source"`
		SourceName  string   `json:"sourceName"`
		Unsupported string   `json:"unsupported"`
		Unsafe      []string `json:"unsafe"`
		Warnings    []string `json:"warnings"`
	}
	get := func(id string) {
		detail = struct {
			Source      string   `json:"source"`
			SourceName  string   `json:"sourceName"`
			Unsupported string   `json:"unsupported"`
			Unsafe      []string `json:"unsafe"`
			Warnings    []string `json:"warnings"`
		}{}
		rec := do(e.h, "GET", "/api/v1/store/apps/"+id, "", e.c, false)
		if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &detail) != nil {
			t.Fatalf("detail %s: %d %s", id, rec.Code, rec.Body)
		}
	}
	get("needs-dep")
	if detail.Source != "acme" || detail.SourceName != "Acme Store" || !strings.Contains(detail.Unsupported, "bitcoin") {
		t.Errorf("needs-dep: %+v", detail)
	}
	get("risky")
	if len(detail.Unsafe) != 1 || detail.Unsupported != "" {
		t.Errorf("risky: %+v", detail)
	}
	get("good")
	if len(detail.Unsafe) != 0 || detail.Warnings == nil {
		t.Errorf("good: %+v", detail)
	}

	if rec := do(e.h, "POST", "/api/v1/store/apps/needs-dep/install", `{}`, e.c, true); rec.Code != 400 || !strings.Contains(rec.Body.String(), "incompatível") {
		t.Errorf("unsupported install: %d %s", rec.Code, rec.Body)
	}
	if rec := do(e.h, "POST", "/api/v1/store/apps/risky/install", `{}`, e.c, true); rec.Code != 400 || !strings.Contains(rec.Body.String(), "allowUnsafe") {
		t.Errorf("unsafe install: %d %s", rec.Code, rec.Body)
	}
	if _, err := os.Stat(e.apps.ProjectDir("risky")); err == nil {
		t.Error("nothing may be written for a rejected install")
	}

	rec := do(e.h, "POST", "/api/v1/store/apps/good/install", `{"port":7100}`, e.c, true)
	if rec.Code != 202 {
		t.Fatalf("install good: %d %s", rec.Code, rec.Body)
	}
	do(e.h, "GET", "/api/v1/store/installs/good/events", "", e.c, false) // waits for the job
	env, err := os.ReadFile(filepath.Join(e.apps.ProjectDir("good"), compose.EnvFile))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(env), "APP_SEED=") || !strings.Contains(string(env), "PORT=7100") {
		t.Errorf(".env: %s", env)
	}
	composeYML, _ := os.ReadFile(filepath.Join(e.apps.ProjectDir("good"), compose.ComposeFile))
	if !strings.Contains(string(composeYML), "omvhome.path: /ui") || strings.Contains(string(composeYML), "x-omvhome") {
		t.Errorf("compose: %s", composeYML)
	}

	if rec := do(e.h, "POST", "/api/v1/store/apps/risky/install", `{"allowUnsafe":true}`, e.c, true); rec.Code != 202 {
		t.Errorf("unsafe install with allowUnsafe: %d %s", rec.Code, rec.Body)
	}
}
