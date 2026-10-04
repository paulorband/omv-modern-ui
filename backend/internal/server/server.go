// Package server wires routes and middleware together.
package server

import (
	"net/http"

	"omvhome/internal/httpx"
	"omvhome/internal/web"
)

// New builds the root handler: /healthz, /api/v1/* and the embedded SPA.
func New() http.Handler {
	root := http.NewServeMux()
	root.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		httpx.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	api := http.NewServeMux()
	api.Handle("/", httpx.Handler(func(http.ResponseWriter, *http.Request) error {
		return httpx.NotFound("rota não encontrada")
	}))
	root.Handle("/api/v1/", http.StripPrefix("/api/v1", api))

	root.Handle("/", web.Handler())
	return root
}
