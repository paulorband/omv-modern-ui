package server

import (
	"context"
	"net/http"
	"time"

	"omvhome/internal/auth"
	"omvhome/internal/httpx"
	"omvhome/internal/omv"
)

// omvCaller binds the authenticated user's session to an omv.Caller.
func (d Deps) omvCaller(r *http.Request) omv.Caller {
	sess := auth.FromContext(r.Context())
	return func(ctx context.Context, service, method string, params, out any) error {
		return d.Auth.Call(ctx, sess, service, method, params, out)
	}
}

func (d Deps) registerSystem(api *http.ServeMux, protect func(http.Handler) http.Handler) {
	api.Handle("GET /system", protect(httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		info, err := d.Collector.Info()
		if err != nil {
			return err
		}
		info.OMVVersion = omv.OMVVersion(r.Context(), d.omvCaller(r))
		httpx.JSON(w, http.StatusOK, info)
		return nil
	})))

	api.Handle("GET /system/stats", protect(httpx.Handler(func(w http.ResponseWriter, _ *http.Request) error {
		st, err := d.Collector.Stats()
		if err != nil {
			return err
		}
		httpx.JSON(w, http.StatusOK, st)
		return nil
	})))

	api.Handle("GET /system/stats/stream", protect(httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		sse, err := httpx.NewSSE(w)
		if err != nil {
			return err
		}
		ch, unsubscribe := d.Hub.Subscribe()
		defer unsubscribe()
		keepAlive := time.NewTicker(25 * time.Second)
		defer keepAlive.Stop()
		for {
			select {
			case <-r.Context().Done():
				return nil
			case st := <-ch:
				if sse.Send("stats", st) != nil {
					return nil
				}
			case <-keepAlive.C:
				if sse.Comment("keep-alive") != nil {
					return nil
				}
			}
		}
	})))

	api.Handle("GET /system/disks", protect(httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		fs, err := omv.ListFilesystems(r.Context(), d.omvCaller(r))
		if err != nil {
			return err
		}
		httpx.JSON(w, http.StatusOK, fs)
		return nil
	})))
}
