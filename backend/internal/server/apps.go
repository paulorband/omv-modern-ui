package server

import (
	"errors"
	"net/http"
	"strconv"

	"omvhome/internal/apps"
	"omvhome/internal/docker"
	"omvhome/internal/httpx"
)

func appsErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, apps.ErrNotFound):
		return httpx.NotFound("app não encontrado")
	case errors.Is(err, apps.ErrNotManaged):
		return httpx.Conflict("%s", err.Error())
	case errors.Is(err, apps.ErrBadID), errors.Is(err, apps.ErrNoPurge):
		return httpx.BadRequest("%s", err.Error())
	}
	return dockerErr(err)
}

func (d Deps) registerApps(api *http.ServeMux, protect func(http.Handler) http.Handler) {
	h := func(fn httpx.Handler) http.Handler { return protect(fn) }

	api.Handle("GET /apps", h(func(w http.ResponseWriter, r *http.Request) error {
		list, err := d.Apps.List(r.Context())
		if err != nil {
			return appsErr(err)
		}
		if list == nil {
			list = []apps.App{}
		}
		httpx.JSON(w, http.StatusOK, list)
		return nil
	}))

	for _, action := range []string{"start", "stop", "restart"} {
		api.Handle("POST /apps/{id}/"+action, h(func(w http.ResponseWriter, r *http.Request) error {
			if err := d.Apps.Control(r.Context(), r.PathValue("id"), action); err != nil {
				return appsErr(err)
			}
			w.WriteHeader(http.StatusNoContent)
			return nil
		}))
	}

	api.Handle("POST /apps/{id}/update", h(func(w http.ResponseWriter, r *http.Request) error {
		if err := d.Apps.Update(r.Context(), r.PathValue("id"), nil); err != nil {
			return appsErr(err)
		}
		w.WriteHeader(http.StatusNoContent)
		return nil
	}))

	api.Handle("DELETE /apps/{id}", h(func(w http.ResponseWriter, r *http.Request) error {
		purge, _ := strconv.ParseBool(r.URL.Query().Get("purgeData"))
		if err := d.Apps.Uninstall(r.Context(), r.PathValue("id"), purge); err != nil {
			return appsErr(err)
		}
		w.WriteHeader(http.StatusNoContent)
		return nil
	}))

	api.Handle("GET /apps/{id}/logs/stream", h(func(w http.ResponseWriter, r *http.Request) error {
		name, err := d.Apps.MainContainer(r.Context(), r.PathValue("id"))
		if err != nil {
			return appsErr(err)
		}
		sse, err := httpx.NewSSE(w)
		if err != nil {
			return err
		}
		err = d.Docker.StreamLogs(r.Context(), name, 100, true, func(l docker.LogLine) error {
			return sse.Send("log", l)
		})
		if err != nil && r.Context().Err() == nil {
			_ = sse.Send("error", map[string]string{"message": "stream de logs interrompido"})
		}
		return nil
	}))
}
