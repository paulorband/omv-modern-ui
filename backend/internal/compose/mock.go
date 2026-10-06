package compose

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
	"omvhome/internal/docker"
)

// MockRunner simulates docker compose on top of docker.Mock: `up` reads the project's
// docker-compose.yml and creates one mock container per service.
type MockRunner struct {
	Docker *docker.Mock
}

func NewMockRunner(d *docker.Mock) *MockRunner { return &MockRunner{Docker: d} }

func (m *MockRunner) Available(context.Context) bool { return true }

func readDotEnv(dir string) map[string]string {
	vars := map[string]string{}
	b, err := os.ReadFile(filepath.Join(dir, EnvFile))
	if err != nil {
		return vars
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if k, v, ok := strings.Cut(line, "="); ok {
			vars[k] = strings.Trim(v, `"`)
		}
	}
	return vars
}

func (m *MockRunner) Up(ctx context.Context, project, dir string, onLine func(string)) error {
	raw, err := os.ReadFile(filepath.Join(dir, ComposeFile))
	if err != nil {
		return err
	}
	var doc struct {
		Services map[string]struct {
			Image         string `yaml:"image"`
			ContainerName string `yaml:"container_name"`
			Ports         []any  `yaml:"ports"`
			Labels        any    `yaml:"labels"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return err
	}
	emit := func(s string) {
		if onLine != nil {
			onLine(s)
		}
	}
	existing, _ := m.Docker.ListContainers(ctx)
	for _, c := range existing {
		if c.Labels[docker.ComposeProject] == project {
			emit("Container " + c.Name + " already exists")
			m.Docker.SetProjectState(project, "running")
			return nil
		}
	}
	vars := readDotEnv(dir)
	names := make([]string, 0, len(doc.Services))
	for n := range doc.Services {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, svcName := range names {
		svc := doc.Services[svcName]
		emit(fmt.Sprintf("Pulling %s (%s)", svcName, svc.Image))
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(80 * time.Millisecond):
		}
		labels := map[string]string{docker.ComposeProject: project, docker.ComposeService: svcName}
		switch l := svc.Labels.(type) {
		case map[string]any:
			for k, v := range l {
				labels[k] = fmt.Sprint(v)
			}
		case []any:
			for _, item := range l {
				if k, v, ok := strings.Cut(fmt.Sprint(item), "="); ok {
					labels[k] = v
				}
			}
		}
		var ports []docker.Port
		for _, p := range svc.Ports {
			parts := strings.Split(Interpolate(fmt.Sprint(p), vars), ":")
			if len(parts) < 2 {
				continue
			}
			pub, _ := strconv.Atoi(parts[len(parts)-2])
			privStr, proto, _ := strings.Cut(parts[len(parts)-1], "/")
			priv, _ := strconv.Atoi(privStr)
			if proto == "" {
				proto = "tcp"
			}
			ports = append(ports, docker.Port{Private: priv, Public: pub, Protocol: proto})
		}
		name := svc.ContainerName
		if name == "" {
			name = svcName
			if len(names) > 1 {
				name = project + "-" + svcName + "-1"
			}
		}
		m.Docker.Add(docker.Detail{
			Container: docker.Container{Name: name, Image: Interpolate(svc.Image, vars), State: "running", Ports: ports, Labels: labels},
			Networks:  []string{project + "_default"}, RestartPolicy: "unless-stopped", Mounts: []docker.Mount{},
		})
		emit("Container " + name + " Started")
	}
	return nil
}

func (m *MockRunner) Start(_ context.Context, project, _ string) error {
	m.Docker.SetProjectState(project, "running")
	return nil
}
func (m *MockRunner) Stop(_ context.Context, project, _ string) error {
	m.Docker.SetProjectState(project, "exited")
	return nil
}
func (m *MockRunner) Restart(_ context.Context, project, _ string) error {
	m.Docker.SetProjectState(project, "running")
	return nil
}
func (m *MockRunner) Pull(_ context.Context, _, _ string, onLine func(string)) error {
	if onLine != nil {
		onLine("Pulled (mock)")
	}
	return nil
}
func (m *MockRunner) Down(_ context.Context, project, _ string) error {
	m.Docker.RemoveByProject(project)
	return nil
}

func (m *MockRunner) SeedDir(context.Context, string, string, string) error { return nil }

func (m *MockRunner) ImageUser(context.Context, string) (int, int, bool) { return 0, 0, false }
