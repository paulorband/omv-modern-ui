// Package server wires routes and middleware together.
package server

import (
	"net/http"

	"omvhome/internal/apps"
	"omvhome/internal/auth"
	"omvhome/internal/catalog"
	"omvhome/internal/compose"
	"omvhome/internal/docker"
	"omvhome/internal/httpx"
	"omvhome/internal/storage"
	"omvhome/internal/store"
	"omvhome/internal/system"
	"omvhome/internal/web"
)

// Deps are the collaborators the routes need.
type Deps struct {
	Auth      *auth.Service
	Collector system.Collector
	Hub       *system.Hub
	Docker    docker.Client
	Compose   compose.Runner
	Apps      *apps.Service
	Catalog   *catalog.Catalog
	Store     *store.Installer
	Storage   *storage.Service
	OMVPort   int
}

// New builds the root handler: /healthz, /api/v1/* and the embedded SPA.
func New(d Deps) http.Handler {
	root := http.NewServeMux()
	root.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		httpx.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	api := http.NewServeMux()
	protect := func(h http.Handler) http.Handler { return d.Auth.Require(h) }

	api.Handle("POST /auth/login", d.Auth.LoginHandler())
	api.Handle("POST /auth/logout", d.Auth.LogoutHandler())
	api.Handle("GET /auth/me", protect(d.Auth.MeHandler()))

	api.Handle("GET /meta", protect(httpx.Handler(func(w http.ResponseWriter, _ *http.Request) error {
		httpx.JSON(w, http.StatusOK, map[string]int{"omvPort": d.OMVPort})
		return nil
	})))
	d.registerSystem(api, protect)
	d.registerContainers(api, protect)
	d.registerApps(api, protect)
	d.registerStore(api, protect)
	d.registerSources(api, protect)
	d.registerStorage(api, protect)

	api.Handle("/", httpx.Handler(func(http.ResponseWriter, *http.Request) error {
		return httpx.NotFound("rota não encontrada")
	}))
	root.Handle("/api/v1/", http.StripPrefix("/api/v1", auth.CSRFGuard(api)))

	root.Handle("/", web.Handler())
	return root
}
