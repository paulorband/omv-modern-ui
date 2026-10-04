package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"omvhome/internal/config"
	"omvhome/internal/server"
)

func main() {
	cfg := config.Load()
	flag.StringVar(&cfg.Listen, "listen", cfg.Listen, "listen address")
	flag.BoolVar(&cfg.Mock, "mock", cfg.Mock, "simulate OMV and Docker (development)")
	flag.StringVar(&cfg.DataDir, "data-dir", cfg.DataDir, "state directory")
	flag.StringVar(&cfg.AppDataDir, "appdata-dir", cfg.AppDataDir, "app volumes directory")
	flag.Parse()
	if os.Getenv("OMVHOME_APPDATA_DIR") == "" && !flagSet("appdata-dir") {
		cfg.AppDataDir = filepath.Join(cfg.DataDir, "appdata") // follow a --data-dir override
	}

	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           server.New(buildDeps(cfg)),
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

func flagSet(name string) bool {
	set := false
	flag.Visit(func(f *flag.Flag) { set = set || f.Name == name })
	return set
}
