package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"omvhome/internal/auth"
	"omvhome/internal/config"
	"omvhome/internal/docker"
	"omvhome/internal/omv"
	"omvhome/internal/server"
	"omvhome/internal/system"
)

func main() {
	cfg := config.Load()
	flag.StringVar(&cfg.Listen, "listen", cfg.Listen, "listen address")
	flag.BoolVar(&cfg.Mock, "mock", cfg.Mock, "simulate OMV and Docker (development)")
	flag.Parse()

	var omvClient omv.Client = omv.NewRPCClient(cfg.OMVURL)
	if cfg.Mock {
		m := omv.NewMock()
		omv.RegisterStorageFixtures(m)
		omvClient = m
	}
	collector := system.New(cfg.Mock)
	var dockerClient docker.Client = docker.NewUnixClient(cfg.DockerSocket)
	if cfg.Mock {
		dockerClient = docker.NewMock()
	}

	srv := &http.Server{
		Addr: cfg.Listen,
		Handler: server.New(server.Deps{
			Auth:      auth.NewService(omvClient),
			Collector: collector,
			Hub:       system.NewHub(collector, 2*time.Second),
			Docker:    dockerClient,
		}),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	slog.Info("omv-home listening", "addr", cfg.Listen, "mock", cfg.Mock)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("server failed", "err", err)
		os.Exit(1)
	}
}
