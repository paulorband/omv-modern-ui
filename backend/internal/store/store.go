// Package store installs apps from templates or pasted compose files (SPEC-05).
package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"omvhome/internal/apps"
	"omvhome/internal/catalog"
	"omvhome/internal/compose"
	"omvhome/internal/docker"
)

// InvalidError is bad user input (HTTP 400); ConflictError is a state clash (HTTP 409).
type (
	InvalidError  struct{ Msg string }
	ConflictError struct{ Msg string }
)

func (e *InvalidError) Error() string  { return e.Msg }
func (e *ConflictError) Error() string { return e.Msg }

func invalid(format string, a ...any) error  { return &InvalidError{fmt.Sprintf(format, a...)} }
func conflict(format string, a ...any) error { return &ConflictError{fmt.Sprintf(format, a...)} }

type Installer struct {
	Catalog *catalog.Catalog
	Apps    *apps.Service
	Docker  docker.Client
	Compose compose.Runner

	// System values exposed to templates as PUID, PGID and TZ.
	PUID, PGID, TZ string

	// PortBusy reports whether a TCP port is taken on the host; MkdirAll creates data folders.
	// Both are replaceable in tests.
	PortBusy func(port int) bool
	MkdirAll func(path string) error
	// Chown hands data folders to the user a container runs as (SPEC-09 R09.10).
	Chown func(path string, uid, gid int) error

	mu   sync.Mutex
	jobs map[string]*Job
}

func NewInstaller(c *catalog.Catalog, a *apps.Service, d docker.Client, r compose.Runner, puid, pgid, tz string) *Installer {
	return &Installer{
		Catalog: c, Apps: a, Docker: d, Compose: r, PUID: puid, PGID: pgid, TZ: tz,
		PortBusy: tcpPortBusy,
		MkdirAll: func(p string) error { return os.MkdirAll(p, 0o755) },
		Chown:    os.Chown,
		jobs:     map[string]*Job{},
	}
}

func tcpPortBusy(port int) bool {
	l, err := net.Listen("tcp", ":"+strconv.Itoa(port))
	if err != nil {
		return true
	}
	_ = l.Close()
	return false
}

type InstallRequest struct {
	Env         map[string]string `json:"env"`
	Port        int               `json:"port"`
	AllowUnsafe bool              `json:"allowUnsafe"`
	// Compose replaces the template compose when the user edited it before installing.
	Compose string `json:"compose"`
}

type CustomRequest struct {
	Name        string `json:"name"`
	Compose     string `json:"compose"`
	MainService string `json:"mainService"`
	Port        int    `json:"port"`
	AllowUnsafe bool   `json:"allowUnsafe"`
}

// Installed returns the ids of installed apps.
func (i *Installer) Installed(ctx context.Context) (map[string]bool, error) {
	list, err := i.Apps.List(ctx)
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, a := range list {
		out[a.ID] = true
	}
	return out, nil
}

// Job returns the latest install job for an app id.
func (i *Installer) Job(id string) (*Job, bool) {
	i.mu.Lock()
	defer i.mu.Unlock()
	j, ok := i.jobs[id]
	return j, ok
}

// Install validates the request and starts an install; progress is available through Job.
func (i *Installer) Install(ctx context.Context, id string, req InstallRequest) (*Job, error) {
	t, err := i.Catalog.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if t.Unsupported != "" {
		return nil, invalid("app incompatível: %s", t.Unsupported)
	}
	composeRaw, unsafe := t.Compose, t.Unsafe
	if req.Compose != "" && req.Compose != t.Compose {
		if len(req.Compose) > 256<<10 {
			return nil, invalid("compose grande demais")
		}
		services, err := serviceNames(req.Compose)
		if err != nil {
			return nil, invalid("%s", err.Error())
		}
		if !slices.Contains(services, t.Meta.Main) {
			return nil, invalid("o serviço principal %q precisa continuar no compose", t.Meta.Main)
		}
		if unsafe, err = unsafeReasons(req.Compose); err != nil {
			return nil, invalid("%s", err.Error())
		}
		composeRaw = req.Compose
	}
	if len(unsafe) > 0 && !req.AllowUnsafe {
		return nil, invalid("configurações inseguras exigem allowUnsafe: %s", strings.Join(unsafe, "; "))
	}
	vars, paths, ports, err := resolveFields(t.Meta, req.Env)
	if err != nil {
		return nil, err
	}
	hostPort := req.Port
	if hostPort == 0 {
		hostPort = t.Meta.Port
	}
	if hostPort < 0 || hostPort > 65535 {
		return nil, invalid("porta inválida: %d", hostPort)
	}
	if hostPort > 0 {
		ports = append([]int{hostPort}, ports...)
	}
	return i.start(ctx, plan{meta: t.Meta, composeRaw: composeRaw, fieldVars: vars, paths: paths, ports: ports, hostPort: hostPort, generate: t.Generate, seed: t.Source != ""})
}

// InstallCustom installs a user-supplied compose file (R05.7).
func (i *Installer) InstallCustom(ctx context.Context, req CustomRequest) (*Job, error) {
	id := slugify(req.Name)
	if !catalog.ValidID(id) {
		return nil, invalid("nome inválido: use letras e números")
	}
	if len(req.Compose) > 256<<10 {
		return nil, invalid("compose grande demais")
	}
	reasons, err := unsafeReasons(req.Compose)
	if err != nil {
		return nil, invalid("%s", err.Error())
	}
	if len(reasons) > 0 && !req.AllowUnsafe {
		return nil, invalid("configurações inseguras exigem allowUnsafe: %s", strings.Join(reasons, "; "))
	}
	main, err := pickMain(req.Compose, req.MainService)
	if err != nil {
		return nil, err
	}
	if req.Port < 0 || req.Port > 65535 {
		return nil, invalid("porta inválida: %d", req.Port)
	}
	var ports []int
	if req.Port > 0 {
		ports = []int{req.Port}
	}
	meta := catalog.Meta{ID: id, Title: strings.TrimSpace(req.Name), Category: "Custom", Port: req.Port, Main: main}
	return i.start(ctx, plan{meta: meta, composeRaw: req.Compose, ports: ports, hostPort: req.Port})
}

func pickMain(raw, requested string) (string, error) {
	services, err := serviceNames(raw)
	if err != nil {
		return "", invalid("%s", err.Error())
	}
	if requested != "" {
		for _, s := range services {
			if s == requested {
				return requested, nil
			}
		}
		return "", invalid("serviço principal %q não existe no compose", requested)
	}
	if len(services) == 1 {
		return services[0], nil
	}
	return "", invalid("informe mainService (serviços: %s)", strings.Join(services, ", "))
}

type plan struct {
	meta       catalog.Meta
	composeRaw string
	fieldVars  []envKV
	paths      []string
	ports      []int
	hostPort   int
	generate   []string // variables filled with random secrets (SPEC-09)
	seed       bool     // copy image defaults into empty data folders (SPEC-09 R09.11)
}

func (i *Installer) start(ctx context.Context, p plan) (*Job, error) {
	id := p.meta.ID
	if !i.Compose.Available(ctx) {
		return nil, compose.ErrUnavailable
	}
	// 1. conflicts, before anything touches the disk (A05.3, A05.4)
	i.mu.Lock()
	if j, ok := i.jobs[id]; ok && !j.Done() {
		i.mu.Unlock()
		return nil, conflict("a instalação de %q já está em andamento", id)
	}
	i.mu.Unlock()
	if _, err := i.Apps.Get(ctx, id); err == nil {
		return nil, conflict("o app %q já está instalado", id)
	} else if !errors.Is(err, apps.ErrNotFound) {
		return nil, err
	}
	if _, err := os.Stat(i.Apps.ProjectDir(id)); err == nil {
		return nil, conflict("já existe um projeto para %q em %s", id, i.Apps.ProjectDir(id))
	}
	if err := i.checkPorts(ctx, p.ports); err != nil {
		return nil, err
	}

	// 2. render
	labels := map[string]string{
		docker.LabelApp: id, docker.LabelName: p.meta.Title, docker.LabelCategory: p.meta.Category,
		docker.LabelMain: p.meta.Main,
	}
	if p.meta.Icon != "" {
		labels[docker.LabelIcon] = p.meta.Icon
	}
	if p.hostPort > 0 {
		labels[docker.LabelPort] = strconv.Itoa(p.hostPort)
	}
	if p.meta.Scheme != "" {
		labels[docker.LabelScheme] = p.meta.Scheme
	}
	if p.meta.Path != "" {
		labels[docker.LabelPath] = p.meta.Path
	}
	rendered, err := injectLabels(p.composeRaw, p.meta.Main, labels)
	if err != nil {
		return nil, invalid("%s", err.Error())
	}
	appData := filepath.Join(i.Apps.AppDataDir, id)
	vars := []envKV{{"APPDATA", appData}, {"PUID", i.PUID}, {"PGID", i.PGID}, {"TZ", i.TZ}}
	if p.hostPort > 0 {
		vars = append(vars, envKV{"PORT", strconv.Itoa(p.hostPort)})
	}
	vars = append(vars, p.fieldVars...)
	for _, key := range p.generate {
		secret, err := randomHex(32)
		if err != nil {
			return nil, err
		}
		vars = append(vars, envKV{key, secret})
	}

	// 3. write project files and data folders
	dir := i.Apps.ProjectDir(id)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(dir, compose.ComposeFile), rendered, 0o644); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(dir, compose.EnvFile), renderDotenv(vars), 0o600); err != nil {
		return nil, err
	}
	for _, d := range append([]string{appData}, p.paths...) {
		if err := i.MkdirAll(d); err != nil {
			_ = os.RemoveAll(dir)
			return nil, fmt.Errorf("criar %s: %w", d, err)
		}
	}
	prep := dataPrep{rendered: rendered, appData: appData, source: p.seed}

	// 4. compose up in the background
	job := newJob(id)
	job.Title, job.Icon = p.meta.Title, p.meta.Icon
	i.mu.Lock()
	i.pruneJobsLocked()
	i.jobs[id] = job
	i.mu.Unlock()
	go i.run(job, id, dir, prep)
	return job, nil
}

func (i *Installer) run(job *Job, id, dir string, prep dataPrep) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	job.emit(Event{Type: "starting", Message: "Iniciando instalação de " + id})
	i.prepareData(ctx, job, prep)
	project := compose.ProjectName(id)
	if err := i.Compose.Up(ctx, project, dir, func(line string) { job.emit(lineEvent(line)) }); err != nil {
		slog.Warn("install failed", "app", id, "err", err)
		// Roll back so the user can retry without a 409 (data folders are kept).
		_ = i.Compose.Down(context.WithoutCancel(ctx), project, dir)
		_ = os.RemoveAll(dir)
		job.emit(Event{Type: "error", Message: err.Error()})
		return
	}
	job.emit(Event{Type: "done", Message: "Instalação concluída"})
}

func (i *Installer) pruneJobsLocked() {
	for id, j := range i.jobs {
		j.mu.Lock()
		stale := j.done && time.Since(j.finishedAt) > jobRetention
		j.mu.Unlock()
		if stale {
			delete(i.jobs, id)
		}
	}
}

// checkPorts fails with a conflict if a port is used by a container or by the host (R05.5).
func (i *Installer) checkPorts(ctx context.Context, ports []int) error {
	if len(ports) == 0 {
		return nil
	}
	cs, err := i.Docker.ListContainers(ctx)
	if err != nil {
		return err
	}
	for _, p := range ports {
		for _, c := range cs {
			for _, cp := range c.Ports {
				if cp.Public == p && cp.Protocol != "udp" {
					return conflict("a porta %d já está em uso pelo container %q", p, c.Name)
				}
			}
		}
		if i.PortBusy(p) {
			return conflict("a porta %d já está em uso no servidor", p)
		}
	}
	return nil
}

// resolveFields validates user values against the template fields (R05.6).
func resolveFields(m catalog.Meta, in map[string]string) (vars []envKV, paths []string, ports []int, err error) {
	for _, f := range m.Env {
		v, given := in[f.Key]
		if !given {
			v = f.Default
		}
		if strings.ContainsAny(v, "\n\r\x00") || len(v) > 512 {
			return nil, nil, nil, invalid("valor inválido para %q", f.Label)
		}
		if v == "" {
			if f.Required {
				return nil, nil, nil, invalid("o campo %q é obrigatório", f.Label)
			}
			vars = append(vars, envKV{f.Key, ""})
			continue
		}
		switch f.Type {
		case "path":
			if !strings.HasPrefix(v, "/") {
				return nil, nil, nil, invalid("%q deve ser um caminho absoluto", f.Label)
			}
			for _, seg := range strings.Split(v, "/") {
				if seg == ".." {
					return nil, nil, nil, invalid("%q não pode conter ..", f.Label)
				}
			}
			v = path.Clean(v)
			paths = append(paths, v)
		case "port":
			n, convErr := strconv.Atoi(v)
			if convErr != nil || n < 1 || n > 65535 {
				return nil, nil, nil, invalid("%q deve ser uma porta entre 1 e 65535", f.Label)
			}
			if !f.Internal {
				ports = append(ports, n)
			}
		case "bool":
			if v != "true" && v != "false" {
				return nil, nil, nil, invalid("%q deve ser true ou false", f.Label)
			}
		case "select":
			ok := false
			for _, o := range f.Options {
				ok = ok || o == v
			}
			if !ok {
				return nil, nil, nil, invalid("valor de %q não está entre as opções", f.Label)
			}
		}
		vars = append(vars, envKV{f.Key, v})
	}
	return vars, paths, ports, nil
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// jobRetention is how long finished installs (and their logs) stay listed (R10.3).
const jobRetention = 24 * time.Hour

// ErrJobNotFound means there is no install job with that id.
var ErrJobNotFound = errors.New("instalação não encontrada")

// Jobs lists known installs, most recent first (R10.1).
func (i *Installer) Jobs() []JobSummary {
	i.mu.Lock()
	i.pruneJobsLocked()
	jobs := make([]*Job, 0, len(i.jobs))
	for _, j := range i.jobs {
		jobs = append(jobs, j)
	}
	i.mu.Unlock()
	out := make([]JobSummary, 0, len(jobs))
	for _, j := range jobs {
		out = append(out, j.Summary())
	}
	sort.Slice(out, func(a, b int) bool { return out[a].StartedAt.After(out[b].StartedAt) })
	return out
}

// Dismiss forgets a finished install and its log (R10.4).
func (i *Installer) Dismiss(id string) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	j, ok := i.jobs[id]
	if !ok {
		return ErrJobNotFound
	}
	if !j.Done() {
		return conflict("a instalação de %q ainda está em andamento", id)
	}
	delete(i.jobs, id)
	return nil
}
