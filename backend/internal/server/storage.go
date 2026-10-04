package server

import (
	"errors"
	"net/http"

	"omvhome/internal/httpx"
	"omvhome/internal/omv"
	"omvhome/internal/storage"
)

func storageErr(err error) error {
	var (
		inv  *storage.InvalidError
		con  *storage.ConflictError
		step *storage.StepError
		hx   *httpx.Error
	)
	switch {
	case err == nil:
		return nil
	case errors.As(err, &inv):
		return httpx.BadRequest("%s", inv.Msg)
	case errors.As(err, &con):
		return httpx.Conflict("%s", con.Msg)
	case errors.Is(err, storage.ErrNotFound):
		return httpx.NotFound("compartilhamento não encontrado")
	case errors.As(err, &step):
		if errors.As(step.Err, &hx) && hx.Status == http.StatusUnauthorized {
			return hx
		}
		return httpx.Upstream("falha na etapa %q do OMV", step.Step)
	}
	return err // already an *httpx.Error from the OMV caller, or an internal error
}

func (d Deps) registerStorage(api *http.ServeMux, protect func(http.Handler) http.Handler) {
	h := func(fn httpx.Handler) http.Handler { return protect(fn) }

	api.Handle("GET /storage/filesystems", h(func(w http.ResponseWriter, r *http.Request) error {
		fs, err := omv.ListFilesystemsWithRefs(r.Context(), d.omvCaller(r))
		if err != nil {
			return err
		}
		httpx.JSON(w, http.StatusOK, fs)
		return nil
	}))

	api.Handle("GET /storage/shares", h(func(w http.ResponseWriter, r *http.Request) error {
		shares, err := d.Storage.List(r.Context(), d.omvCaller(r))
		if err != nil {
			return storageErr(err)
		}
		httpx.JSON(w, http.StatusOK, shares)
		return nil
	}))

	api.Handle("POST /storage/shares", h(func(w http.ResponseWriter, r *http.Request) error {
		var in storage.CreateInput
		if err := httpx.Decode(r, &in); err != nil {
			return err
		}
		sh, err := d.Storage.Create(r.Context(), d.omvCaller(r), in)
		if err != nil {
			return storageErr(err)
		}
		httpx.JSON(w, http.StatusCreated, sh)
		return nil
	}))

	api.Handle("PATCH /storage/shares/{uuid}", h(func(w http.ResponseWriter, r *http.Request) error {
		var in storage.UpdateInput
		if err := httpx.Decode(r, &in); err != nil {
			return err
		}
		sh, err := d.Storage.Update(r.Context(), d.omvCaller(r), r.PathValue("uuid"), in)
		if err != nil {
			return storageErr(err)
		}
		httpx.JSON(w, http.StatusOK, sh)
		return nil
	}))

	api.Handle("DELETE /storage/shares/{uuid}", h(func(w http.ResponseWriter, r *http.Request) error {
		if err := d.Storage.Delete(r.Context(), d.omvCaller(r), r.PathValue("uuid")); err != nil {
			return storageErr(err)
		}
		w.WriteHeader(http.StatusNoContent)
		return nil
	}))

	api.Handle("GET /storage/users", h(func(w http.ResponseWriter, r *http.Request) error {
		users, err := omv.ListUsers(r.Context(), d.omvCaller(r))
		if err != nil {
			return err
		}
		if users == nil {
			users = []omv.User{}
		}
		httpx.JSON(w, http.StatusOK, users)
		return nil
	}))
}
