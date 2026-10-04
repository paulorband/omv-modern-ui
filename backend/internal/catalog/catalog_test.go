package catalog

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const validTpl = `
x-omvhome:
  id: demo
  title: Demo
  main: web
  port: 8080
  env:
    - { key: DIR, label: Dir, type: path, default: /srv/x, required: true }
services:
  web:
    image: nginx
`

func TestParseValid(t *testing.T) { // A05.1
	tpl, err := Parse([]byte(validTpl))
	if err != nil {
		t.Fatal(err)
	}
	if tpl.Meta.ID != "demo" || tpl.Meta.Category != "Other" || len(tpl.Meta.Env) != 1 || tpl.Meta.Env[0].Type != "path" {
		t.Fatalf("%+v", tpl.Meta)
	}
}

func TestParseRejects(t *testing.T) { // A05.1
	cases := map[string]string{
		"missing id":       "x-omvhome:\n  main: web\nservices:\n  web: {image: x}\n",
		"invalid id":       "x-omvhome:\n  id: Bad_ID\n  main: web\nservices:\n  web: {image: x}\n",
		"missing main":     "x-omvhome:\n  id: a\nservices:\n  web: {image: x}\n",
		"main not in svcs": "x-omvhome:\n  id: a\n  main: db\nservices:\n  web: {image: x}\n",
		"bad field type":   "x-omvhome:\n  id: a\n  main: web\n  env:\n    - {key: A, type: nope}\nservices:\n  web: {image: x}\n",
		"reserved key":     "x-omvhome:\n  id: a\n  main: web\n  env:\n    - {key: APPDATA, type: path}\nservices:\n  web: {image: x}\n",
		"select no opts":   "x-omvhome:\n  id: a\n  main: web\n  env:\n    - {key: A, type: select}\nservices:\n  web: {image: x}\n",
		"not yaml":         ":\n\t- [",
	}
	for name, raw := range cases {
		if _, err := Parse([]byte(raw)); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestEmbeddedCatalogHasRequiredApps(t *testing.T) { // R05.10
	c, err := New("")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"jellyfin", "pihole", "vaultwarden", "syncthing", "uptime-kuma", "qbittorrent", "homeassistant", "nextcloud"} {
		tpl, err := c.Get(context.Background(), id)
		if err != nil {
			t.Errorf("%s: %v", id, err)
			continue
		}
		if !strings.Contains(tpl.Compose, "${PORT:-") && tpl.Meta.Port > 0 {
			t.Errorf("%s: host port must use ${PORT:-default} so the installer can override it", id)
		}
	}
}

func TestListFilters(t *testing.T) {
	c, _ := New("")
	ctx := context.Background()
	if all := c.List(ctx, "", ""); len(all) < 8 || all[0].Title > all[1].Title {
		t.Fatalf("list: %d (sorted by title expected)", len(all))
	}
	if got := c.List(ctx, "JELLY", ""); len(got) != 1 || got[0].ID != "jellyfin" {
		t.Errorf("query: %+v", got)
	}
	for _, s := range c.List(ctx, "", "files") {
		if !strings.EqualFold(s.Category, "Files") {
			t.Errorf("category filter leaked %+v", s)
		}
	}
	if _, err := c.Get(ctx, "nope"); err != ErrNotFound {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

func TestRemoteCatalogMergesAndSurvivesFailures(t *testing.T) { // R05.8
	remoteTpl := strings.Replace(validTpl, "id: demo", "id: remote-app", 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/index.json", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`["` + "http://" + r.Host + `/ok.yml","` + "http://" + r.Host + `/broken.yml","` + "http://" + r.Host + `/missing.yml"]`))
	})
	mux.HandleFunc("/ok.yml", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(remoteTpl)) })
	mux.HandleFunc("/broken.yml", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("not: [valid")) })
	srv := httptest.NewServer(mux)
	c, _ := New(srv.URL + "/index.json")
	ctx := context.Background()

	if _, err := c.Get(ctx, "remote-app"); err != nil {
		t.Fatalf("remote template missing: %v", err)
	}
	if _, err := c.Get(ctx, "jellyfin"); err != nil {
		t.Fatalf("embedded must still be there: %v", err)
	}

	srv.Close() // remote now unreachable
	c2, _ := New(srv.URL + "/index.json")
	if got := c2.List(ctx, "", ""); len(got) < 8 {
		t.Fatalf("embedded catalog must survive a dead remote, got %d", len(got))
	}
}
