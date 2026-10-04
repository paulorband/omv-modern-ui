package server

import (
	"errors"
	"net/http"

	"omvhome/internal/catalog"
	"omvhome/internal/compose"
	"omvhome/internal/httpx"
	"omvhome/internal/store"
)

func storeErr(err error) error {
	var inv *store.InvalidError
	var con *store.ConflictError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &inv):
		return httpx.BadRequest("%s", inv.Msg)
	case errors.As(err, &con):
		return httpx.Conflict("%s", con.Msg)
	case errors.Is(err, catalog.ErrNotFound):
		return httpx.NotFound("app não encontrado no catálogo")
	case errors.Is(err, compose.ErrUnavailable):
		return httpx.Upstream("%s", compose.ErrUnavailable.Error())
	}
	return appsErr(err)
}

func (d Deps) registerStore(api *http.ServeMux, protect func(http.Handler) http.Handler) {
	h := func(fn httpx.Handler) http.Handler { return protect(fn) }

	api.Handle("GET /store/apps", h(func(w http.ResponseWriter, r *http.Request) error {
		installed, err := d.Store.Installed(r.Context())
		if err != nil {
			return storeErr(err)
		}
		type card struct {
			catalog.Summary
			Installed bool `json:"installed"`
		}
		list := d.Catalog.List(r.Context(), r.URL.Query().Get("q"), r.URL.Query().Get("category"))
		out := make([]card, 0, len(list))
		for _, s := range list {
			out = append(out, card{Summary: s, Installed: installed[s.ID]})
		}
		httpx.JSON(w, http.StatusOK, out)
		return nil
	}))

	api.Handle("GET /store/apps/{id}", h(func(w http.ResponseWriter, r *http.Request) error {
		t, err := d.Catalog.Get(r.Context(), r.PathValue("id"))
		if err != nil {
			return storeErr(err)
		}
		installed, err := d.Store.Installed(r.Context())
		if err != nil {
			return storeErr(err)
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"meta": t.Meta, "compose": t.Compose, "installed": installed[t.Meta.ID]})
		return nil
	}))

	api.Handle("POST /store/apps/{id}/install", h(func(w http.ResponseWriter, r *http.Request) error {
		var req store.InstallRequest
		if err := httpx.Decode(r, &req); err != nil {
			return err
		}
		job, err := d.Store.Install(r.Context(), r.PathValue("id"), req)
		if err != nil {
			return storeErr(err)
		}
		httpx.JSON(w, http.StatusAccepted, map[string]string{"jobId": job.ID})
		return nil
	}))

	api.Handle("POST /store/custom", h(func(w http.ResponseWriter, r *http.Request) error {
		var req store.CustomRequest
		if err := httpx.Decode(r, &req); err != nil {
			return err
		}
		job, err := d.Store.InstallCustom(r.Context(), req)
		if err != nil {
			return storeErr(err)
		}
		httpx.JSON(w, http.StatusAccepted, map[string]string{"jobId": job.ID})
		return nil
	}))

	api.Handle("GET /store/installs/{id}/events", h(func(w http.ResponseWriter, r *http.Request) error {
		job, ok := d.Store.Job(r.PathValue("id"))
		if !ok {
			return httpx.NotFound("nenhuma instalação encontrada para este app")
		}
		sse, err := httpx.NewSSE(w)
		if err != nil {
			return err
		}
		_ = job.Stream(r.Context(), func(e store.Event) error { return sse.Send(e.Type, e) })
		return nil
	}))
}
