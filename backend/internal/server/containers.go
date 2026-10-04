package server

import (
	"bytes"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"omvhome/internal/docker"
	"omvhome/internal/httpx"
)

var imageIDRe = regexp.MustCompile(`^(sha256:)?[a-f0-9]{12,64}$`)

// dockerErr maps Docker client errors to API errors.
func dockerErr(err error) error {
	var ve *docker.ValidationError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &ve):
		return httpx.BadRequest("%s", ve.Msg)
	case errors.Is(err, docker.ErrNotFound):
		return httpx.NotFound("não encontrado")
	case errors.Is(err, docker.ErrUnavailable):
		return httpx.Upstream("Docker indisponível")
	}
	if st, ok := docker.ErrorStatus(err); ok && st == http.StatusConflict {
		return httpx.Conflict("%s", err.Error())
	}
	return httpx.Upstream("%s", err.Error())
}

func containerRef(r *http.Request) (string, error) {
	id := r.PathValue("id")
	if !docker.ValidRef(id) {
		return "", httpx.BadRequest("identificador de container inválido")
	}
	return id, nil
}

func (d Deps) registerContainers(api *http.ServeMux, protect func(http.Handler) http.Handler) {
	h := func(fn httpx.Handler) http.Handler { return protect(fn) }

	api.Handle("GET /docker/status", h(func(w http.ResponseWriter, r *http.Request) error {
		resp := map[string]any{"available": false}
		if v, err := d.Docker.Ping(r.Context()); err != nil {
			resp["error"] = "Docker indisponível"
		} else {
			resp["available"], resp["version"], resp["apiVersion"] = true, v.Version, v.APIVersion
		}
		httpx.JSON(w, http.StatusOK, resp)
		return nil
	}))

	api.Handle("GET /containers", h(func(w http.ResponseWriter, r *http.Request) error {
		cs, err := d.Docker.ListContainers(r.Context())
		if err != nil {
			return dockerErr(err)
		}
		httpx.JSON(w, http.StatusOK, cs)
		return nil
	}))

	api.Handle("GET /containers/{id}", h(func(w http.ResponseWriter, r *http.Request) error {
		id, err := containerRef(r)
		if err != nil {
			return err
		}
		det, err := d.Docker.InspectContainer(r.Context(), id)
		if err != nil {
			return dockerErr(err)
		}
		httpx.JSON(w, http.StatusOK, det)
		return nil
	}))

	for _, action := range []string{"start", "stop", "restart"} {
		api.Handle("POST /containers/{id}/"+action, h(func(w http.ResponseWriter, r *http.Request) error {
			id, err := containerRef(r)
			if err != nil {
				return err
			}
			switch action {
			case "start":
				err = d.Docker.StartContainer(r.Context(), id)
			case "stop":
				err = d.Docker.StopContainer(r.Context(), id)
			default:
				err = d.Docker.RestartContainer(r.Context(), id)
			}
			if err != nil {
				return dockerErr(err)
			}
			w.WriteHeader(http.StatusNoContent)
			return nil
		}))
	}

	api.Handle("DELETE /containers/{id}", h(func(w http.ResponseWriter, r *http.Request) error {
		id, err := containerRef(r)
		if err != nil {
			return err
		}
		det, err := d.Docker.InspectContainer(r.Context(), id)
		if err != nil {
			return dockerErr(err)
		}
		if det.AppID != "" {
			return httpx.Conflict("este container pertence ao app %q; desinstale o app para removê-lo", det.AppID)
		}
		force, _ := strconv.ParseBool(r.URL.Query().Get("force"))
		volumes, _ := strconv.ParseBool(r.URL.Query().Get("volumes"))
		if err := d.Docker.RemoveContainer(r.Context(), id, force, volumes); err != nil {
			return dockerErr(err)
		}
		w.WriteHeader(http.StatusNoContent)
		return nil
	}))

	api.Handle("GET /containers/{id}/logs", h(func(w http.ResponseWriter, r *http.Request) error {
		id, err := containerRef(r)
		if err != nil {
			return err
		}
		tail := 200
		if v := r.URL.Query().Get("tail"); v != "" {
			n, perr := strconv.Atoi(v)
			if perr != nil || n < 1 || n > 10000 {
				return httpx.BadRequest("tail deve estar entre 1 e 10000")
			}
			tail = n
		}
		var buf bytes.Buffer
		if err := d.Docker.StreamLogs(r.Context(), id, tail, false, func(l docker.LogLine) error {
			buf.WriteString(l.Text)
			buf.WriteByte('\n')
			return nil
		}); err != nil {
			return dockerErr(err)
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write(buf.Bytes())
		return nil
	}))

	api.Handle("GET /containers/{id}/logs/stream", h(func(w http.ResponseWriter, r *http.Request) error {
		id, err := containerRef(r)
		if err != nil {
			return err
		}
		// Fail with a proper status before switching to an event stream.
		if _, err := d.Docker.InspectContainer(r.Context(), id); err != nil {
			return dockerErr(err)
		}
		sse, err := httpx.NewSSE(w)
		if err != nil {
			return err
		}
		err = d.Docker.StreamLogs(r.Context(), id, 100, true, func(l docker.LogLine) error {
			return sse.Send("log", l)
		})
		if err != nil && r.Context().Err() == nil {
			_ = sse.Send("error", map[string]string{"message": "stream de logs interrompido"})
		}
		return nil
	}))

	api.Handle("GET /containers/{id}/stats", h(func(w http.ResponseWriter, r *http.Request) error {
		id, err := containerRef(r)
		if err != nil {
			return err
		}
		st, err := d.Docker.ContainerStats(r.Context(), id)
		if err != nil {
			return dockerErr(err)
		}
		httpx.JSON(w, http.StatusOK, st)
		return nil
	}))

	api.Handle("POST /containers/run", h(func(w http.ResponseWriter, r *http.Request) error {
		var spec docker.RunSpec
		if err := httpx.Decode(r, &spec); err != nil {
			return err
		}
		if spec.Labels != nil {
			return httpx.BadRequest("labels não são aceitos neste endpoint")
		}
		id, err := d.Docker.Run(r.Context(), spec, nil)
		if err != nil {
			return dockerErr(err)
		}
		httpx.JSON(w, http.StatusCreated, map[string]string{"id": id})
		return nil
	}))

	api.Handle("POST /containers/import-run", h(func(w http.ResponseWriter, r *http.Request) error {
		var in struct {
			Command string `json:"command"`
		}
		if err := httpx.Decode(r, &in); err != nil {
			return err
		}
		spec, err := docker.ParseRunCommand(in.Command)
		if err != nil {
			return dockerErr(err)
		}
		httpx.JSON(w, http.StatusOK, spec)
		return nil
	}))

	api.Handle("GET /images", h(func(w http.ResponseWriter, r *http.Request) error {
		imgs, err := d.Docker.ListImages(r.Context())
		if err != nil {
			return dockerErr(err)
		}
		httpx.JSON(w, http.StatusOK, imgs)
		return nil
	}))

	api.Handle("POST /images/pull", h(func(w http.ResponseWriter, r *http.Request) error {
		var in struct {
			Ref string `json:"ref"`
		}
		if err := httpx.Decode(r, &in); err != nil {
			return err
		}
		if in.Ref == "" || len(in.Ref) > 255 {
			return httpx.BadRequest("referência de imagem inválida")
		}
		sse, err := httpx.NewSSE(w)
		if err != nil {
			return err
		}
		start := time.Now()
		if err := d.Docker.PullImage(r.Context(), in.Ref, func(ev docker.PullEvent) { _ = sse.Send("progress", ev) }); err != nil {
			if r.Context().Err() == nil {
				_ = sse.Send("error", map[string]string{"message": dockerErr(err).Error()})
			}
			return nil
		}
		_ = sse.Send("done", map[string]any{"ref": in.Ref, "seconds": int(time.Since(start).Seconds())})
		return nil
	}))

	api.Handle("DELETE /images/{id}", h(func(w http.ResponseWriter, r *http.Request) error {
		id := r.PathValue("id")
		if !imageIDRe.MatchString(id) {
			return httpx.BadRequest("identificador de imagem inválido")
		}
		if err := d.Docker.RemoveImage(r.Context(), id); err != nil {
			return dockerErr(err)
		}
		w.WriteHeader(http.StatusNoContent)
		return nil
	}))
}
