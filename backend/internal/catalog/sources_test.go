package catalog

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func makeZip(t *testing.T, top string, files map[string]string) []byte {
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

const umbrelStore = `id: "acme"
name: "Acme Store"
`

var umbrelFiles = map[string]string{
	"umbrel-app-store.yml": umbrelStore,
	"README.md":            "# readme",
	"acme-web/umbrel-app.yml": `manifestVersion: 1
id: acme-web
name: Acme Web
tagline: Um app de teste
category: finance
port: 8080
path: "/admin"
description: Descrição longa.
defaultUsername: admin
defaultPassword: hunter2
dependencies:
`,
	"acme-web/docker-compose.yml": `version: '3.7'
services:
  app_proxy:
    environment:
      APP_HOST: acme-web_web_1
      APP_PORT: 80
  db:
    image: mariadb:10
    volumes:
      - ${APP_DATA_DIR}/db:/var/lib/mysql
  web:
    image: acme/web:1.0
    depends_on: [db, app_proxy]
    environment:
      - DB_HOST=acme-web_db_1
      - SEED=${APP_SEED}
      - ID=${APP_ID}
    volumes:
      - ${APP_DATA_DIR}/data:/data
`,
	"acme-dep/umbrel-app.yml":      "id: acme-dep\nname: Dep\nport: 1\ndependencies:\n  - bitcoin\n",
	"acme-dep/docker-compose.yml":  "services:\n  app_proxy:\n    environment:\n      APP_HOST: acme-dep_web_1\n      APP_PORT: 1\n  web:\n    image: x\n",
	"acme-var/umbrel-app.yml":      "id: acme-var\nname: Var\nport: 2\n",
	"acme-var/docker-compose.yml":  "services:\n  app_proxy:\n    environment:\n      APP_HOST: acme-var_web_1\n      APP_PORT: 2\n  web:\n    image: x\n    environment:\n      NODE: ${APP_BITCOIN_NODE_IP}\n",
	"acme-priv/umbrel-app.yml":     "id: acme-priv\nname: Priv\nport: 3\n",
	"acme-priv/docker-compose.yml": "services:\n  app_proxy:\n    environment:\n      APP_HOST: acme-priv_web_1\n      APP_PORT: 3\n  web:\n    image: x\n    privileged: true\n    cap_add: [NET_ADMIN]\n",
}

const casaosApp = `name: foo-app
services:
  app:
    image: foo/app:1
    environment:
      PUID: $PUID
      PGID: $PGID
      NAME: $AppID
    ports:
      - target: 80
        published: "8081"
        protocol: tcp
    volumes:
      - type: bind
        source: /DATA/AppData/foo-app/config
        target: /config
      - /DATA/Media/Movies:/movies
      - /DATA/Media/Movies:/movies2:ro
    x-casaos:
      envs:
        - container: PUID
          description:
            en_US: user id
x-casaos:
  architectures: [amd64, arm64, arm]
  main: app
  port_map: "8081"
  index: /web
  category: Media
  icon: https://example.com/foo.png
  title:
    en_US: Foo App
    pt_BR: Aplicativo Foo
  tagline:
    en_US: Does foo
  description:
    en_US: Long foo
`

var casaosFiles = map[string]string{
	"Apps/Foo/docker-compose.yml": casaosApp,
	"Apps/Bad/docker-compose.yml": "name: bad\nservices:\n  app:\n    image: x\n    network_mode: host\nx-casaos:\n  main: app\n  title:\n    en_US: Bad\n",
	"Apps/Var/docker-compose.yml": "name: varapp\nservices:\n  app:\n    image: x\n    environment:\n      P: $default_pwd\nx-casaos:\n  main: app\n  title:\n    en_US: Var\n",
	"category-list.json":          "[]",
}

func mustLoad(t *testing.T, z []byte) (string, string, map[string]*Template) {
	t.Helper()
	f, n, apps, err := loadRepo(z, "src")
	if err != nil {
		t.Fatal(err)
	}
	return f, n, apps
}

func TestUmbrelConversion(t *testing.T) { // A09.1, A09.3, A09.4
	format, name, apps := mustLoad(t, makeZip(t, "acme-main/", umbrelFiles))
	if format != FormatUmbrel || name != "Acme Store" || len(apps) != 4 {
		t.Fatalf("format=%s name=%s apps=%d", format, name, len(apps))
	}
	web := apps["acme-web"]
	if web == nil || web.Unsupported != "" {
		t.Fatalf("acme-web: %+v", web)
	}
	if web.Meta.Main != "web" || web.Meta.Port != 8080 || web.Meta.Path != "/admin" || web.Meta.Category != "Finance" || web.Source != "src" {
		t.Fatalf("meta: %+v", web.Meta)
	}
	if !strings.Contains(web.Meta.Description, "hunter2") {
		t.Fatalf("default credentials missing from description: %q", web.Meta.Description)
	}
	c := web.Compose
	for _, want := range []string{"${PORT:-8080}:80", "${APPDATA}/db:/var/lib/mysql", "${APPDATA}/data:/data", "DB_HOST=db", "ID=acme-web", "SEED=${APP_SEED}"} {
		if !strings.Contains(c, want) {
			t.Errorf("compose missing %q:\n%s", want, c)
		}
	}
	for _, bad := range []string{"app_proxy", "APP_DATA_DIR", "_db_1"} {
		if strings.Contains(c, bad) {
			t.Errorf("compose still has %q:\n%s", bad, c)
		}
	}
	if len(web.Generate) != 1 || web.Generate[0] != "APP_SEED" {
		t.Errorf("generate: %v", web.Generate)
	}
	if apps["acme-dep"].Unsupported == "" || !strings.Contains(apps["acme-dep"].Unsupported, "bitcoin") {
		t.Errorf("dependencies: %q", apps["acme-dep"].Unsupported)
	}
	if u := apps["acme-var"].Unsupported; !strings.Contains(u, "APP_BITCOIN_NODE_IP") {
		t.Errorf("unknown var: %q", u)
	}
	priv := apps["acme-priv"]
	if priv.Unsupported != "" || len(priv.Unsafe) != 1 || !strings.Contains(priv.Unsafe[0], "privileged") || len(priv.Warnings) != 1 {
		t.Errorf("priv: unsupported=%q unsafe=%v warnings=%v", priv.Unsupported, priv.Unsafe, priv.Warnings)
	}
}

func TestCasaosConversion(t *testing.T) { // A09.2, A09.3
	format, _, apps := mustLoad(t, makeZip(t, "store-main/", casaosFiles))
	if format != FormatCasaos || len(apps) != 3 {
		t.Fatalf("format=%s apps=%d", format, len(apps))
	}
	foo := apps["foo-app"]
	if foo == nil || foo.Unsupported != "" {
		t.Fatalf("foo: %+v", foo)
	}
	m := foo.Meta
	if m.Title != "Aplicativo Foo" || m.Tagline != "Does foo" || m.Port != 8081 || m.Path != "/web" || m.Category != "Media" || m.Main != "app" {
		t.Fatalf("meta: %+v", m)
	}
	if len(m.Env) != 1 || m.Env[0].Key != "DATA_1" || m.Env[0].Type != "path" || !m.Env[0].Required {
		t.Fatalf("env fields (same source must dedupe): %+v", m.Env)
	}
	for _, want := range []string{"${APPDATA}/config", "${DATA_1}:/movies", "${DATA_1}:/movies2:ro", "published: ${PORT:-8081}", "NAME: foo-app"} {
		if !strings.Contains(foo.Compose, want) {
			t.Errorf("compose missing %q:\n%s", want, foo.Compose)
		}
	}
	if body, _, _ := strings.Cut(foo.Compose, "x-omvhome:"); strings.Contains(body, "x-casaos") || strings.Contains(body, "/DATA/") {
		t.Errorf("leftovers:\n%s", foo.Compose)
	}
	if apps["bad"].Unsupported != "" || len(apps["bad"].Unsafe) != 1 {
		t.Errorf("bad: %+v", apps["bad"])
	}
	if !strings.Contains(apps["varapp"].Unsupported, "default_pwd") {
		t.Errorf("varapp: %q", apps["varapp"].Unsupported)
	}
}

func TestLoadRepoRejectsUnknownFormat(t *testing.T) { // A09.6
	_, _, _, err := loadRepo(makeZip(t, "x/", map[string]string{"README.md": "hi"}), "")
	var inv *SourceInvalidError
	if !errors.As(err, &inv) {
		t.Fatalf("want SourceInvalidError, got %v", err)
	}
	if _, _, _, err := loadRepo([]byte("not a zip"), ""); !errors.As(err, &inv) {
		t.Fatalf("not a zip: %v", err)
	}
}

func TestDownloadURL(t *testing.T) { // A09.6
	cases := []struct{ in, canonical, zipURL string }{
		{"https://github.com/WisdomSky/LinuxServer-AppStore", "https://github.com/WisdomSky/LinuxServer-AppStore", "https://github.com/WisdomSky/LinuxServer-AppStore/archive/HEAD.zip"},
		{"https://github.com/o/r.git/", "https://github.com/o/r", "https://github.com/o/r/archive/HEAD.zip"},
		{"https://github.com/o/r/tree/dev", "https://github.com/o/r/tree/dev", "https://github.com/o/r/archive/refs/heads/dev.zip"},
		{"http://nas.local/stores/mine.zip?x=1", "http://nas.local/stores/mine.zip", "http://nas.local/stores/mine.zip"},
	}
	for _, c := range cases {
		d, err := downloadURL(c.in)
		if err != nil || d.canonical != c.canonical || d.zipURL != c.zipURL {
			t.Errorf("%s → %+v, %v", c.in, d, err)
		}
	}
	for _, bad := range []string{"", "ftp://x/y.zip", "file:///etc/passwd", "javascript:alert(1)", "https://example.com/page", "https://github.com/onlyowner"} {
		var inv *SourceInvalidError
		if _, err := downloadURL(bad); !errors.As(err, &inv) {
			t.Errorf("%q should be invalid, got %v", bad, err)
		}
	}
}

// storeServer serves zips by path and lets tests break it.
type storeServer struct {
	*httptest.Server
	zips map[string][]byte
	down bool
}

func newStoreServer(t *testing.T, zips map[string][]byte) *storeServer {
	s := &storeServer{zips: zips}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		z, ok := s.zips[r.URL.Path]
		if !ok || s.down {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(z)
	}))
	t.Cleanup(s.Close)
	return s
}

func newCatalog(t *testing.T, dir string) *Catalog {
	t.Helper()
	c, err := New("")
	if err != nil {
		t.Fatal(err)
	}
	if err := c.UseDir(dir); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestSourceLifecycle(t *testing.T) { // A09.5, A09.7
	ctx := context.Background()
	srv := newStoreServer(t, map[string][]byte{
		"/umbrel.zip": makeZip(t, "umbrel-main/", umbrelFiles),
		"/casaos.zip": makeZip(t, "casaos-main/", casaosFiles),
		"/plain.zip":  makeZip(t, "x/", map[string]string{"README.md": "hi"}),
	})
	dir := t.TempDir()
	c := newCatalog(t, dir)

	s, err := c.AddSource(ctx, srv.URL+"/umbrel.zip", "")
	if err != nil {
		t.Fatal(err)
	}
	if s.ID != "umbrel" || s.Name != "Acme Store" || s.Format != FormatUmbrel || s.Apps != 4 {
		t.Fatalf("source: %+v", s)
	}
	if _, err := os.Stat(filepath.Join(dir, "sources", "umbrel.zip")); err != nil {
		t.Fatalf("zip not cached: %v", err)
	}
	var conflict *SourceConflictError
	if _, err := c.AddSource(ctx, srv.URL+"/umbrel.zip", ""); !errors.As(err, &conflict) {
		t.Fatalf("duplicate: %v", err)
	}
	var invalid *SourceInvalidError
	if _, err := c.AddSource(ctx, srv.URL+"/plain.zip", ""); !errors.As(err, &invalid) {
		t.Fatalf("plain: %v", err)
	}
	var fetch *SourceFetchError
	if _, err := c.AddSource(ctx, srv.URL+"/missing.zip", ""); !errors.As(err, &fetch) {
		t.Fatalf("missing: %v", err)
	}
	if _, err := c.AddSource(ctx, srv.URL+"/casaos.zip", "Minha loja"); err != nil {
		t.Fatal(err)
	}

	// listing, filters, summaries
	if n := len(c.List(ctx, "", "", "umbrel")); n != 4 {
		t.Errorf("umbrel apps: %d", n)
	}
	if n := len(c.List(ctx, "", "", BuiltinSource)); n < 8 {
		t.Errorf("builtin apps: %d", n)
	}
	var found *Summary
	for _, sm := range c.List(ctx, "", "", "umbrel") {
		if sm.ID == "acme-dep" {
			found = &sm
		}
	}
	if found == nil || !found.Unsupported || found.Source != "umbrel" {
		t.Errorf("summary: %+v", found)
	}
	if got, err := c.Get(ctx, "foo-app"); err != nil || got.Source != "casaos" {
		t.Errorf("get foo-app: %+v %v", got, err)
	}

	// a fresh catalog loads the cache without network (A09.5)
	srv.down = true
	c2 := newCatalog(t, dir)
	if len(c2.Sources()) != 2 || c2.Sources()[1].Name != "Minha loja" {
		t.Fatalf("reloaded sources: %+v", c2.Sources())
	}
	if _, err := c2.Get(ctx, "acme-web"); err != nil {
		t.Fatalf("reloaded app: %v", err)
	}

	// failed refresh keeps the previous catalog (A09.7)
	if _, err := c2.RefreshSource(ctx, "umbrel"); !errors.As(err, &fetch) {
		t.Fatalf("refresh: %v", err)
	}
	if _, err := c2.Get(ctx, "acme-web"); err != nil {
		t.Fatalf("catalog lost after failed refresh: %v", err)
	}
	if got := c2.Sources()[0]; got.Error == "" || got.Apps != 4 {
		t.Errorf("source after failed refresh: %+v", got)
	}
	srv.down = false
	if got, err := c2.RefreshSource(ctx, "umbrel"); err != nil || got.Error != "" {
		t.Fatalf("refresh ok: %+v %v", got, err)
	}

	// removal deletes metadata and zip
	if err := c2.RemoveSource("umbrel"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "sources", "umbrel.zip")); !os.IsNotExist(err) {
		t.Errorf("zip should be gone: %v", err)
	}
	if _, err := c2.Get(ctx, "acme-web"); !errors.Is(err, ErrNotFound) {
		t.Errorf("app should be gone: %v", err)
	}
	if err := c2.RemoveSource("umbrel"); !errors.Is(err, ErrSourceNotFound) {
		t.Errorf("remove twice: %v", err)
	}
	if len(newCatalog(t, dir).Sources()) != 1 {
		t.Error("removal not persisted")
	}
}

func TestIDClashes(t *testing.T) { // R09.7
	ctx := context.Background()
	clash := func(id string) map[string]string {
		return map[string]string{
			"umbrel-app-store.yml":     "id: s\nname: S\n",
			id + "/umbrel-app.yml":     "id: " + id + "\nname: " + id + "\nport: 9\n",
			id + "/docker-compose.yml": "services:\n  app_proxy:\n    environment:\n      APP_HOST: " + id + "_web_1\n      APP_PORT: 9\n  web:\n    image: x\n",
		}
	}
	srv := newStoreServer(t, map[string][]byte{
		"/a.zip": makeZip(t, "a/", clash("jellyfin")),
		"/b.zip": makeZip(t, "b/", clash("shared")),
		"/c.zip": makeZip(t, "c/", clash("shared")),
	})
	c := newCatalog(t, t.TempDir())
	for _, p := range []string{"/a.zip", "/b.zip", "/c.zip"} {
		if _, err := c.AddSource(ctx, srv.URL+p, ""); err != nil {
			t.Fatal(err)
		}
	}
	if got, _ := c.Get(ctx, "jellyfin"); got == nil || got.Source != "" {
		t.Errorf("built-in must win: %+v", got)
	}
	if got, err := c.Get(ctx, "jellyfin-a"); err != nil || got.Source != "a" || got.Meta.ID != "jellyfin-a" {
		t.Errorf("jellyfin-a: %+v %v", got, err)
	}
	if got, _ := c.Get(ctx, "shared"); got == nil || got.Source != "b" {
		t.Errorf("shared: %+v", got)
	}
	if got, err := c.Get(ctx, "shared-c"); err != nil || got.Source != "c" {
		t.Errorf("shared-c: %+v %v", got, err)
	}
}

func TestRefreshStale(t *testing.T) { // R09.8
	ctx := context.Background()
	srv := newStoreServer(t, map[string][]byte{"/u.zip": makeZip(t, "u/", umbrelFiles)})
	c := newCatalog(t, t.TempDir())
	if _, err := c.AddSource(ctx, srv.URL+"/u.zip", ""); err != nil {
		t.Fatal(err)
	}
	before := c.Sources()[0].UpdatedAt
	time.Sleep(5 * time.Millisecond)
	c.RefreshStale(ctx, time.Hour) // fresh: untouched
	if !c.Sources()[0].UpdatedAt.Equal(before) {
		t.Error("fresh source was refreshed")
	}
	c.RefreshStale(ctx, time.Nanosecond)
	if !c.Sources()[0].UpdatedAt.After(before) {
		t.Error("stale source was not refreshed")
	}
	srv.down = true
	c.RefreshStale(ctx, time.Nanosecond) // only logs
}

func TestUmbrelAppWithoutProxyPublishesItsOwnPort(t *testing.T) {
	_, _, apps := mustLoad(t, makeZip(t, "s/", map[string]string{
		"umbrel-app-store.yml":          "id: s\nname: S\n",
		"direct-app/umbrel-app.yml":     "id: direct-app\nname: Direct\nport: 8096\n",
		"direct-app/docker-compose.yml": "services:\n  db:\n    image: postgres\n  web:\n    image: x\n    ports:\n      - \"8096:8080\"\n",
	}))
	a := apps["direct-app"]
	if a == nil || a.Unsupported != "" || a.Meta.Main != "web" || a.Meta.Port != 8096 {
		t.Fatalf("direct-app: %+v", a)
	}
	if !strings.Contains(a.Compose, "${PORT:-8096}:8080") {
		t.Errorf("published port not parameterized:\n%s", a.Compose)
	}
}
