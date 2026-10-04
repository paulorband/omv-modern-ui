package main

import (
	"time"

	"omvhome/internal/apps"
	"omvhome/internal/auth"
	"omvhome/internal/compose"
	"omvhome/internal/config"
	"omvhome/internal/docker"
	"omvhome/internal/omv"
	"omvhome/internal/server"
	"omvhome/internal/system"
)

// buildDeps wires real or simulated (--mock) collaborators.
func buildDeps(cfg config.Config) server.Deps {
	collector := system.New(cfg.Mock)

	var (
		omvClient    omv.Client = omv.NewRPCClient(cfg.OMVURL)
		dockerClient docker.Client
		composeRun   compose.Runner
	)
	if cfg.Mock {
		m := omv.NewMock()
		omv.RegisterStorageFixtures(m)
		omvClient = m
		dm := docker.NewMock()
		dockerClient, composeRun = dm, compose.NewMockRunner(dm)
	} else {
		dockerClient, composeRun = docker.NewUnixClient(cfg.DockerSocket), compose.NewExecRunner()
	}

	return server.Deps{
		Auth:      auth.NewService(omvClient),
		Collector: collector,
		Hub:       system.NewHub(collector, 2*time.Second),
		Docker:    dockerClient,
		Compose:   composeRun,
		Apps: &apps.Service{
			Docker: dockerClient, Compose: composeRun, DataDir: cfg.DataDir, AppDataDir: cfg.AppDataDir,
		},
	}
}
