// Package docker is a minimal Docker Engine API client (HTTP over the unix socket) plus the
// domain types the REST layer exposes (SPEC-03).
package docker

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"
)

// Labels written on app containers (SPEC-04).
const (
	LabelApp      = "omvhome.app"
	LabelName     = "omvhome.name"
	LabelIcon     = "omvhome.icon"
	LabelPort     = "omvhome.port"
	LabelScheme   = "omvhome.scheme"
	LabelPath     = "omvhome.path"
	LabelCategory = "omvhome.category"
	LabelMain     = "omvhome.main"
	// ComposeProject is the label docker compose puts on every container of a project.
	ComposeProject = "com.docker.compose.project"
	ComposeService = "com.docker.compose.service"
)

var (
	// ErrNotFound is returned when the Docker daemon answers 404.
	ErrNotFound = errors.New("docker: not found")
	// ErrUnavailable wraps connection failures (daemon down, socket missing).
	ErrUnavailable = errors.New("docker: unavailable")
)

var nameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`)

// ValidRef reports whether s is safe to place in a Docker API path (container id or name) (R03.11).
func ValidRef(s string) bool { return len(s) <= 128 && nameRe.MatchString(s) }

// Port is a published port mapping.
type Port struct {
	Private  int    `json:"private"`
	Public   int    `json:"public"`
	Protocol string `json:"protocol"`
}

// Container is one row of the container list.
type Container struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Image     string            `json:"image"`
	State     string            `json:"state"`
	Status    string            `json:"status"`
	Ports     []Port            `json:"ports"`
	CreatedAt time.Time         `json:"createdAt"`
	AppID     string            `json:"appId,omitempty"`
	Labels    map[string]string `json:"-"`
}

type Mount struct {
	Type        string `json:"type"`
	Source      string `json:"source"`
	Destination string `json:"destination"`
	ReadOnly    bool   `json:"readOnly"`
}

// Detail is the inspect view (R03.5). Env values of sensitive keys are masked.
type Detail struct {
	Container
	Env           []EnvVar `json:"env"`
	Mounts        []Mount  `json:"mounts"`
	Networks      []string `json:"networks"`
	RestartPolicy string   `json:"restartPolicy"`
	Tty           bool     `json:"tty"`
	Command       []string `json:"command"`
}

type EnvVar struct {
	Key    string `json:"key"`
	Value  string `json:"value"`
	Masked bool   `json:"masked"`
}

const maskedValue = "••••••••"

var sensitiveKey = regexp.MustCompile(`(?i)(PASSWORD|SECRET|TOKEN|KEY)`)

// ParseEnv converts "K=V" strings, masking sensitive values.
func ParseEnv(env []string) []EnvVar {
	out := make([]EnvVar, 0, len(env))
	for _, e := range env {
		k, v, _ := strings.Cut(e, "=")
		ev := EnvVar{Key: k, Value: v}
		if sensitiveKey.MatchString(k) {
			ev.Value, ev.Masked = maskedValue, true
		}
		out = append(out, ev)
	}
	return out
}

// Stats is a single resource sample (R03.6).
type Stats struct {
	CPUPercent  float64 `json:"cpuPercent"`
	MemoryUsed  uint64  `json:"memoryUsed"`
	MemoryLimit uint64  `json:"memoryLimit"`
	NetRx       uint64  `json:"netRx"`
	NetTx       uint64  `json:"netTx"`
}

type Image struct {
	ID        string    `json:"id"`
	Tags      []string  `json:"tags"`
	Size      int64     `json:"size"`
	CreatedAt time.Time `json:"createdAt"`
	InUse     bool      `json:"inUse"`
}

type Version struct {
	Version    string `json:"version"`
	APIVersion string `json:"apiVersion"`
}

// RunPort / RunVolume / RunSpec describe a container to create (R03.7).
type RunPort struct {
	HostIP    string `json:"hostIp,omitempty"`
	Host      int    `json:"host"` // 0 = let Docker pick
	Container int    `json:"container"`
	Protocol  string `json:"protocol,omitempty"` // tcp (default) or udp
}

type RunVolume struct {
	Host      string `json:"host"`
	Container string `json:"container"`
	ReadOnly  bool   `json:"readOnly"`
}

type RunSpec struct {
	Image      string            `json:"image"`
	Name       string            `json:"name,omitempty"`
	Ports      []RunPort         `json:"ports,omitempty"`
	Volumes    []RunVolume       `json:"volumes,omitempty"`
	Env        map[string]string `json:"env,omitempty"`
	Restart    string            `json:"restart,omitempty"`
	Network    string            `json:"network,omitempty"`
	User       string            `json:"user,omitempty"`
	Hostname   string            `json:"hostname,omitempty"`
	Privileged bool              `json:"privileged,omitempty"`
	Command    []string          `json:"command,omitempty"`
	Labels     map[string]string `json:"labels,omitempty"`
}

// PullEvent is one progress line of an image pull.
type PullEvent struct {
	Status   string `json:"status"`
	ID       string `json:"id,omitempty"`
	Progress string `json:"progress,omitempty"`
	Error    string `json:"error,omitempty"`
}

// LogLine is one demultiplexed log line.
type LogLine struct {
	Stream string `json:"stream"` // stdout | stderr
	Text   string `json:"text"`
}

// Client is the subset of Docker the app needs. RealClient talks to a daemon; Mock simulates one.
type Client interface {
	Ping(ctx context.Context) (Version, error)
	ListContainers(ctx context.Context) ([]Container, error)
	InspectContainer(ctx context.Context, id string) (*Detail, error)
	StartContainer(ctx context.Context, id string) error
	StopContainer(ctx context.Context, id string) error
	RestartContainer(ctx context.Context, id string) error
	RemoveContainer(ctx context.Context, id string, force, volumes bool) error
	// StreamLogs calls fn for each line; with follow it blocks until ctx is done.
	StreamLogs(ctx context.Context, id string, tail int, follow bool, fn func(LogLine) error) error
	ContainerStats(ctx context.Context, id string) (Stats, error)
	// Run creates and starts a container, pulling the image if missing. Returns the new container id.
	Run(ctx context.Context, spec RunSpec, onPull func(PullEvent)) (string, error)
	ListImages(ctx context.Context) ([]Image, error)
	PullImage(ctx context.Context, ref string, onEvent func(PullEvent)) error
	RemoveImage(ctx context.Context, id string) error
}
