// Package apps turns labelled containers / compose projects into "apps" (SPEC-04).
package apps

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"omvhome/internal/compose"
	"omvhome/internal/docker"
)

var (
	ErrNotFound   = errors.New("app não encontrado")
	ErrNotManaged = errors.New("app não foi instalado pela loja")
	ErrBadID      = errors.New("identificador de app inválido")
	ErrNoPurge    = errors.New("diretório de dados inválido para remoção")
)

var idRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,39}$`)

// ValidID reports whether id is a legal app id.
func ValidID(id string) bool { return idRe.MatchString(id) }

type App struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Icon       string   `json:"icon"`
	Category   string   `json:"category"`
	State      string   `json:"state"` // running | stopped | partial
	Port       int      `json:"port"`
	Scheme     string   `json:"scheme"`
	Path       string   `json:"path"`
	Managed    bool     `json:"managed"`
	Containers []string `json:"containers"`
}

type Service struct {
	Docker     docker.Client
	Compose    compose.Runner
	DataDir    string // holds apps/<id>/docker-compose.yml
	AppDataDir string // holds <id>/ volumes
}

// ProjectDir is where a managed app's compose file and .env live.
func (s *Service) ProjectDir(id string) string { return filepath.Join(s.DataDir, "apps", id) }

func (s *Service) hasProjectFiles(id string) bool {
	_, err := os.Stat(filepath.Join(s.ProjectDir(id), compose.ComposeFile))
	return err == nil
}

// AggregateState implements A04.2.
func AggregateState(states []string) string {
	running := 0
	for _, st := range states {
		if st == "running" {
			running++
		}
	}
	switch {
	case len(states) == 0 || running == 0:
		return "stopped"
	case running == len(states):
		return "running"
	default:
		return "partial"
	}
}

// Group builds apps from a container list (pure, testable) (A04.1).
func Group(cs []docker.Container) []App {
	byProject := map[string][]docker.Container{}
	for _, c := range cs {
		if p := c.Labels[docker.ComposeProject]; p != "" {
			byProject[p] = append(byProject[p], c)
		}
	}
	apps := map[string]*App{}
	members := map[string]map[string]docker.Container{} // appID -> name -> container
	add := func(id string, c docker.Container) {
		if members[id] == nil {
			members[id] = map[string]docker.Container{}
		}
		members[id][c.Name] = c
	}
	for _, c := range cs {
		id := c.Labels[docker.LabelApp]
		if id == "" || !ValidID(id) {
			continue
		}
		if apps[id] == nil {
			port, _ := strconv.Atoi(c.Labels[docker.LabelPort])
			a := &App{
				ID: id, Name: firstNonEmpty(c.Labels[docker.LabelName], id), Icon: c.Labels[docker.LabelIcon],
				Category: firstNonEmpty(c.Labels[docker.LabelCategory], "Other"), Port: port,
				Scheme: firstNonEmpty(c.Labels[docker.LabelScheme], "http"), Path: c.Labels[docker.LabelPath],
				Managed: c.Labels[docker.ComposeProject] == compose.ProjectName(id),
			}
			apps[id] = a
		}
		add(id, c)
		if p := c.Labels[docker.ComposeProject]; p != "" {
			for _, sib := range byProject[p] { // siblings (databases, workers) belong to the app too
				add(id, sib)
			}
		}
	}
	out := make([]App, 0, len(apps))
	for id, a := range apps {
		var states []string
		for name, c := range members[id] {
			a.Containers = append(a.Containers, name)
			states = append(states, c.State)
		}
		sort.Strings(a.Containers)
		a.State = AggregateState(states)
		out = append(out, *a)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func (s *Service) List(ctx context.Context) ([]App, error) {
	cs, err := s.Docker.ListContainers(ctx)
	if err != nil {
		return nil, err
	}
	return Group(cs), nil
}

// Get returns one app.
func (s *Service) Get(ctx context.Context, id string) (*App, error) {
	if !ValidID(id) {
		return nil, ErrBadID
	}
	list, err := s.List(ctx)
	if err != nil {
		return nil, err
	}
	for i := range list {
		if list[i].ID == id {
			return &list[i], nil
		}
	}
	return nil, ErrNotFound
}

// Control starts, stops or restarts every container of the app.
func (s *Service) Control(ctx context.Context, id, action string) error {
	a, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if a.Managed && s.hasProjectFiles(id) {
		dir, project := s.ProjectDir(id), compose.ProjectName(id)
		switch action {
		case "start":
			return s.Compose.Start(ctx, project, dir)
		case "stop":
			return s.Compose.Stop(ctx, project, dir)
		case "restart":
			return s.Compose.Restart(ctx, project, dir)
		}
		return fmt.Errorf("ação desconhecida: %s", action)
	}
	for _, name := range a.Containers {
		var err error
		switch action {
		case "start":
			err = s.Docker.StartContainer(ctx, name)
		case "stop":
			err = s.Docker.StopContainer(ctx, name)
		case "restart":
			err = s.Docker.RestartContainer(ctx, name)
		default:
			return fmt.Errorf("ação desconhecida: %s", action)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// Update pulls newer images and recreates the containers (managed apps only) (R04.4).
func (s *Service) Update(ctx context.Context, id string, onLine func(string)) error {
	a, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if !a.Managed || !s.hasProjectFiles(id) {
		return ErrNotManaged
	}
	dir, project := s.ProjectDir(id), compose.ProjectName(id)
	if err := s.Compose.Pull(ctx, project, dir, onLine); err != nil {
		return err
	}
	return s.Compose.Up(ctx, project, dir, onLine)
}

// Uninstall removes the app; purge also deletes its data directory (R04.5).
func (s *Service) Uninstall(ctx context.Context, id string, purge bool) error {
	a, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	var dataDir string
	if purge {
		if dataDir, err = s.safeDataDir(id); err != nil {
			return err // validate before destroying anything
		}
	}
	if a.Managed && s.hasProjectFiles(id) {
		if err := s.Compose.Down(ctx, compose.ProjectName(id), s.ProjectDir(id)); err != nil {
			return err
		}
		if err := os.RemoveAll(s.ProjectDir(id)); err != nil {
			return err
		}
	} else {
		for _, name := range a.Containers {
			if err := s.Docker.RemoveContainer(ctx, name, true, false); err != nil {
				return err
			}
		}
	}
	if purge {
		return os.RemoveAll(dataDir)
	}
	return nil
}

// safeDataDir returns AppDataDir/<id> after checking it is strictly inside AppDataDir (A04.4).
func (s *Service) safeDataDir(id string) (string, error) {
	root := filepath.Clean(s.AppDataDir)
	if !ValidID(id) || s.AppDataDir == "" || root == "." || root == string(filepath.Separator) || filepath.Dir(root) == root {
		return "", ErrNoPurge
	}
	target := filepath.Join(root, id)
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", ErrNoPurge
	}
	return target, nil
}

// MainContainer returns the name of the container that carries the app labels (for logs).
func (s *Service) MainContainer(ctx context.Context, id string) (string, error) {
	cs, err := s.Docker.ListContainers(ctx)
	if err != nil {
		return "", err
	}
	if !ValidID(id) {
		return "", ErrBadID
	}
	for _, c := range cs {
		if c.Labels[docker.LabelApp] == id {
			return c.Name, nil
		}
	}
	return "", ErrNotFound
}
