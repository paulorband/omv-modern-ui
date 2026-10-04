package docker

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

type mockEntry struct {
	detail Detail
	logs   []string
}

// Mock is an in-memory Docker daemon for --mock and tests. It also exposes Add/Remove helpers
// so the compose mock can create containers.
type Mock struct {
	mu         sync.Mutex
	seq        int
	containers []*mockEntry
	images     []Image
	// Now is overridable for tests.
	Now func() time.Time
}

func NewMock() *Mock {
	m := &Mock{Now: time.Now}
	m.seed()
	return m
}

func (m *Mock) nextID() string {
	m.seq++
	return fmt.Sprintf("%012x", 0xabc000000000+m.seq*7919)
}

func appLabels(id, name, icon string, port int, category string) map[string]string {
	return map[string]string{
		LabelApp: id, LabelName: name, LabelIcon: icon, LabelPort: fmt.Sprint(port),
		LabelCategory: category, LabelMain: id,
		ComposeProject: "omvhome-" + id, ComposeService: id,
	}
}

func (m *Mock) seed() {
	now := m.Now()
	add := func(name, image, state string, labels map[string]string, ports []Port, env []string) {
		m.Add(Detail{
			Container: Container{Name: name, Image: image, State: state, Ports: ports, Labels: labels, CreatedAt: now.Add(-48 * time.Hour)},
			Env:       ParseEnv(env), Networks: []string{"bridge"}, RestartPolicy: "unless-stopped",
			Mounts: []Mount{{Type: "bind", Source: "/srv/appdata/" + name, Destination: "/config"}},
		})
	}
	add("jellyfin", "jellyfin/jellyfin:latest", "running", appLabels("jellyfin", "Jellyfin", "emoji:🎬", 8096, "Media"),
		[]Port{{Private: 8096, Public: 8096, Protocol: "tcp"}}, []string{"TZ=America/Sao_Paulo"})
	add("pihole", "pihole/pihole:latest", "running", appLabels("pihole", "Pi-hole", "emoji:🛡️", 8081, "Network"),
		[]Port{{Private: 80, Public: 8081, Protocol: "tcp"}, {Private: 53, Public: 53, Protocol: "udp"}}, []string{"WEBPASSWORD=hunter2"})
	add("syncthing", "syncthing/syncthing:latest", "exited", appLabels("syncthing", "Syncthing", "emoji:🔄", 8384, "Files"),
		[]Port{{Private: 8384, Public: 8384, Protocol: "tcp"}}, nil)
	add("nginx-test", "nginx:alpine", "running", nil, []Port{{Private: 80, Public: 8088, Protocol: "tcp"}}, []string{"NGINX_HOST=localhost"})
	add("postgres-old", "postgres:15", "exited", nil, nil, []string{"POSTGRES_PASSWORD=secret", "POSTGRES_DB=app"})

	m.images = []Image{
		{ID: "a1b2c3d4e5f6", Tags: []string{"jellyfin/jellyfin:latest"}, Size: 780 << 20, CreatedAt: now.Add(-72 * time.Hour), InUse: true},
		{ID: "b2c3d4e5f6a1", Tags: []string{"pihole/pihole:latest"}, Size: 310 << 20, CreatedAt: now.Add(-96 * time.Hour), InUse: true},
		{ID: "c3d4e5f6a1b2", Tags: []string{"nginx:alpine"}, Size: 45 << 20, CreatedAt: now.Add(-24 * time.Hour), InUse: true},
		{ID: "d4e5f6a1b2c3", Tags: []string{"hello-world:latest"}, Size: 20 << 10, CreatedAt: now.Add(-500 * time.Hour)},
	}
}

// Add registers a container (used by seeds and the compose mock) and returns its id.
func (m *Mock) Add(d Detail) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.addLocked(d)
}

func (m *Mock) addLocked(d Detail) string {
	d.ID = m.nextID()
	if d.CreatedAt.IsZero() {
		d.CreatedAt = m.Now()
	}
	d.AppID = d.Labels[LabelApp]
	if d.Ports == nil {
		d.Ports = []Port{}
	}
	d.Status = statusText(d.State)
	logs := []string{
		"[init] starting " + d.Name,
		"[info] listening on configured ports",
		"[warn] this is simulated output from --mock",
	}
	m.containers = append(m.containers, &mockEntry{detail: d, logs: logs})
	return d.ID
}

func statusText(state string) string {
	if state == "running" {
		return "Up 2 hours"
	}
	return "Exited (0) 3 hours ago"
}

func (m *Mock) find(id string) (*mockEntry, error) {
	for _, e := range m.containers {
		if e.detail.ID == id || e.detail.Name == id || (len(id) >= 4 && strings.HasPrefix(e.detail.ID, id)) {
			return e, nil
		}
	}
	return nil, fmt.Errorf("%w: container %s", ErrNotFound, id)
}

// RemoveByProject deletes every container of a compose project (compose mock `down`).
func (m *Mock) RemoveByProject(project string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	kept := m.containers[:0]
	for _, e := range m.containers {
		if e.detail.Labels[ComposeProject] != project {
			kept = append(kept, e)
		}
	}
	m.containers = kept
}

// SetProjectState starts/stops every container of a compose project.
func (m *Mock) SetProjectState(project, state string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range m.containers {
		if e.detail.Labels[ComposeProject] == project {
			e.detail.State, e.detail.Status = state, statusText(state)
		}
	}
}

func (m *Mock) Ping(context.Context) (Version, error) {
	return Version{Version: "27.0.0-mock", APIVersion: "1.46"}, nil
}

func (m *Mock) ListContainers(context.Context) ([]Container, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Container, 0, len(m.containers))
	for _, e := range m.containers {
		out = append(out, e.detail.Container)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (m *Mock) InspectContainer(_ context.Context, id string) (*Detail, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, err := m.find(id)
	if err != nil {
		return nil, err
	}
	d := e.detail
	return &d, nil
}

func (m *Mock) setState(id, state string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, err := m.find(id)
	if err != nil {
		return err
	}
	e.detail.State, e.detail.Status = state, statusText(state)
	return nil
}

func (m *Mock) StartContainer(_ context.Context, id string) error   { return m.setState(id, "running") }
func (m *Mock) StopContainer(_ context.Context, id string) error    { return m.setState(id, "exited") }
func (m *Mock) RestartContainer(_ context.Context, id string) error { return m.setState(id, "running") }

func (m *Mock) RemoveContainer(_ context.Context, id string, force, _ bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, err := m.find(id)
	if err != nil {
		return err
	}
	if e.detail.State == "running" && !force {
		return &apiError{Status: 409, Message: "cannot remove a running container"}
	}
	for i, x := range m.containers {
		if x == e {
			m.containers = append(m.containers[:i], m.containers[i+1:]...)
			break
		}
	}
	return nil
}

func (m *Mock) StreamLogs(ctx context.Context, id string, tail int, follow bool, fn func(LogLine) error) error {
	m.mu.Lock()
	e, err := m.find(id)
	var lines []string
	if err == nil {
		lines = append(lines, e.logs...)
	}
	m.mu.Unlock()
	if err != nil {
		return err
	}
	if tail > 0 && len(lines) > tail {
		lines = lines[len(lines)-tail:]
	}
	for _, l := range lines {
		stream := "stdout"
		if strings.HasPrefix(l, "[warn]") {
			stream = "stderr"
		}
		if err := fn(LogLine{Stream: stream, Text: l}); err != nil {
			return err
		}
	}
	if !follow {
		return nil
	}
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for n := 1; ; n++ {
		select {
		case <-ctx.Done():
			return nil
		case t := <-tick.C:
			if err := fn(LogLine{Stream: "stdout", Text: fmt.Sprintf("[info] %s heartbeat #%d", t.Format("15:04:05"), n)}); err != nil {
				return err
			}
		}
	}
}

func (m *Mock) ContainerStats(_ context.Context, id string) (Stats, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, err := m.find(id); err != nil {
		return Stats{}, err
	}
	return Stats{CPUPercent: 3.2, MemoryUsed: 180 << 20, MemoryLimit: 8 << 30, NetRx: 12 << 20, NetTx: 3 << 20}, nil
}

func (m *Mock) Run(ctx context.Context, spec RunSpec, onPull func(PullEvent)) (string, error) {
	if err := spec.Validate(); err != nil {
		return "", err
	}
	m.mu.Lock()
	have := false
	for _, im := range m.images {
		for _, t := range im.Tags {
			if t == spec.Image || t == spec.Image+":latest" {
				have = true
			}
		}
	}
	m.mu.Unlock()
	if !have {
		if err := m.PullImage(ctx, spec.Image, onPull); err != nil {
			return "", err
		}
	}
	name := spec.Name
	if name == "" {
		name = "mock-" + strings.NewReplacer("/", "-", ":", "-").Replace(spec.Image)
	}
	d := Detail{
		Container: Container{Name: name, Image: spec.Image, State: "running", Labels: spec.Labels},
		Networks:  []string{"bridge"}, RestartPolicy: spec.Restart, Mounts: []Mount{},
	}
	for k, v := range spec.Env {
		d.Env = append(d.Env, ParseEnv([]string{k + "=" + v})...)
	}
	for _, p := range spec.Ports {
		proto := p.Protocol
		if proto == "" {
			proto = "tcp"
		}
		d.Ports = append(d.Ports, Port{Private: p.Container, Public: p.Host, Protocol: proto})
	}
	for _, v := range spec.Volumes {
		d.Mounts = append(d.Mounts, Mount{Type: "bind", Source: v.Host, Destination: v.Container, ReadOnly: v.ReadOnly})
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.addLocked(d), nil
}

func (m *Mock) ListImages(context.Context) ([]Image, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Image(nil), m.images...), nil
}

func (m *Mock) PullImage(ctx context.Context, ref string, onEvent func(PullEvent)) error {
	steps := []PullEvent{
		{Status: "Pulling from " + ref},
		{Status: "Downloading", ID: "layer1", Progress: "[=====>     ] 12MB/40MB"},
		{Status: "Downloading", ID: "layer1", Progress: "[==========>] 40MB/40MB"},
		{Status: "Pull complete", ID: "layer1"},
		{Status: "Status: Downloaded newer image for " + ref},
	}
	for _, ev := range steps {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(60 * time.Millisecond):
		}
		if onEvent != nil {
			onEvent(ev)
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.images = append(m.images, Image{ID: fmt.Sprintf("%012x", 0xfeed00000000+len(m.images)), Tags: []string{ref}, Size: 40 << 20, CreatedAt: m.Now()})
	return nil
}

func (m *Mock) RemoveImage(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, im := range m.images {
		if im.ID == id {
			if im.InUse {
				return &apiError{Status: 409, Message: "image is being used by a container"}
			}
			m.images = append(m.images[:i], m.images[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("%w: image %s", ErrNotFound, id)
}
