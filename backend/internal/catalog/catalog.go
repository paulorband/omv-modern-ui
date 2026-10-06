// Package catalog parses app templates (compose + x-omvhome) and serves the embedded and
// optional remote catalog (SPEC-05).
package catalog

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

//go:embed templates/*.yml
var embedded embed.FS

var ErrNotFound = errors.New("app não encontrado no catálogo")

var idRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,39}$`)

// ValidID mirrors apps.ValidID (kept local to avoid an import cycle).
func ValidID(id string) bool { return idRe.MatchString(id) }

// Field types accepted in x-omvhome.env.
var fieldTypes = map[string]bool{"string": true, "path": true, "port": true, "password": true, "bool": true, "select": true}

type EnvField struct {
	Key      string   `yaml:"key" json:"key"`
	Label    string   `yaml:"label" json:"label"`
	Type     string   `yaml:"type" json:"type"`
	Default  string   `yaml:"default" json:"default"`
	Required bool     `yaml:"required" json:"required"`
	Options  []string `yaml:"options,omitempty" json:"options,omitempty"`
	// Internal marks a "port" field that is a container port: it is not checked against host ports.
	Internal bool `yaml:"internal,omitempty" json:"internal,omitempty"`
}

type Meta struct {
	ID          string     `yaml:"id" json:"id"`
	Title       string     `yaml:"title" json:"title"`
	Tagline     string     `yaml:"tagline" json:"tagline"`
	Description string     `yaml:"description" json:"description"`
	Icon        string     `yaml:"icon" json:"icon"`
	Category    string     `yaml:"category" json:"category"`
	Port        int        `yaml:"port" json:"port"`
	Main        string     `yaml:"main" json:"main"`
	Scheme      string     `yaml:"scheme,omitempty" json:"scheme,omitempty"`
	Path        string     `yaml:"path,omitempty" json:"path,omitempty"`
	Env         []EnvField `yaml:"env" json:"env"`
}

// Template is a parsed app template; Compose holds the original YAML.
//
// Templates that come from a custom source (SPEC-09) also carry where they came from, why they
// cannot be installed (Unsupported), risky settings (Unsafe, Warnings) and the variables the
// installer must generate (Generate).
type Template struct {
	Meta        Meta     `json:"meta"`
	Compose     string   `json:"compose"`
	Source      string   `json:"source,omitempty"`
	Unsupported string   `json:"unsupported,omitempty"`
	Unsafe      []string `json:"unsafe,omitempty"`
	Warnings    []string `json:"warnings,omitempty"`
	Generate    []string `json:"-"`
}

// Summary is one catalog card.
type Summary struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Tagline  string `json:"tagline"`
	Icon     string `json:"icon"`
	Category string `json:"category"`
	// Source is the custom source id ("" for built-in and remote-index apps).
	Source      string `json:"source,omitempty"`
	Unsupported bool   `json:"unsupported,omitempty"`
}

// Parse validates and parses a template (A05.1).
func Parse(raw []byte) (*Template, error) {
	var doc struct {
		Meta     Meta                      `yaml:"x-omvhome"`
		Services map[string]map[string]any `yaml:"services"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("template inválido: %w", err)
	}
	m := doc.Meta
	if m.ID == "" || !ValidID(m.ID) {
		return nil, fmt.Errorf("template inválido: x-omvhome.id ausente ou inválido")
	}
	if m.Main == "" {
		return nil, fmt.Errorf("template %s: x-omvhome.main ausente", m.ID)
	}
	if _, ok := doc.Services[m.Main]; !ok {
		return nil, fmt.Errorf("template %s: serviço principal %q não existe em services", m.ID, m.Main)
	}
	if m.Port < 0 || m.Port > 65535 {
		return nil, fmt.Errorf("template %s: porta inválida", m.ID)
	}
	if m.Title == "" {
		m.Title = m.ID
	}
	if m.Category == "" {
		m.Category = "Other"
	}
	seen := map[string]bool{}
	for _, f := range m.Env {
		if f.Key == "" || seen[f.Key] || ReservedVars[f.Key] || !fieldTypes[f.Type] {
			return nil, fmt.Errorf("template %s: campo env inválido (%q, tipo %q)", m.ID, f.Key, f.Type)
		}
		if f.Type == "select" && len(f.Options) == 0 {
			return nil, fmt.Errorf("template %s: campo %s do tipo select sem options", m.ID, f.Key)
		}
		seen[f.Key] = true
	}
	doc.Meta = m
	return &Template{Meta: m, Compose: string(raw)}, nil
}

const remoteTTL = 10 * time.Minute

// Catalog serves embedded templates plus an optional remote index.
type Catalog struct {
	embedded  map[string]*Template
	remoteURL string
	hc        *http.Client

	mu        sync.Mutex
	remote    map[string]*Template
	fetchedAt time.Time
	now       func() time.Time

	// custom sources (SPEC-09)
	zipClient  *http.Client
	smu        sync.RWMutex
	sdir       string
	sources    []*sourceState
	sourceApps map[string]*Template
}

// New loads the embedded templates; remoteURL (may be empty) points to an index.json.
func New(remoteURL string) (*Catalog, error) {
	c := &Catalog{embedded: map[string]*Template{}, remoteURL: remoteURL, hc: &http.Client{Timeout: 10 * time.Second}, zipClient: &http.Client{Timeout: 2 * time.Minute}, now: time.Now}
	entries, err := embedded.ReadDir("templates")
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		raw, err := embedded.ReadFile("templates/" + e.Name())
		if err != nil {
			return nil, err
		}
		t, err := Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", e.Name(), err)
		}
		c.embedded[t.Meta.ID] = t
	}
	return c, nil
}

// all merges embedded and remote (embedded wins on id clashes).
func (c *Catalog) all(ctx context.Context) map[string]*Template {
	out := c.sourceTemplates()
	for id, t := range c.remoteTemplates(ctx) {
		out[id] = t
	}
	for id, t := range c.embedded {
		out[id] = t
	}
	return out
}

func (c *Catalog) remoteTemplates(ctx context.Context) map[string]*Template {
	if c.remoteURL == "" {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.remote != nil && c.now().Sub(c.fetchedAt) < remoteTTL {
		return c.remote
	}
	fetched, err := c.fetchRemote(ctx)
	if err != nil {
		slog.Warn("catálogo remoto indisponível", "err", err) // embedded catalog keeps working (R05.8)
		if c.remote != nil {
			return c.remote
		}
		return nil
	}
	c.remote, c.fetchedAt = fetched, c.now()
	return c.remote
}

func (c *Catalog) get(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d em %s", resp.StatusCode, url)
	}
	return io.ReadAll(io.LimitReader(resp.Body, limit))
}

// fetchRemote reads index.json — a JSON array of template URLs — and parses each template.
func (c *Catalog) fetchRemote(ctx context.Context) (map[string]*Template, error) {
	raw, err := c.get(ctx, c.remoteURL, 1<<20)
	if err != nil {
		return nil, err
	}
	var urls []string
	if err := json.Unmarshal(raw, &urls); err != nil {
		return nil, fmt.Errorf("index.json inválido: %w", err)
	}
	out := map[string]*Template{}
	for _, u := range urls {
		body, err := c.get(ctx, u, 1<<20)
		if err != nil {
			slog.Warn("template remoto ignorado", "url", u, "err", err)
			continue
		}
		t, err := Parse(body)
		if err != nil {
			slog.Warn("template remoto inválido", "url", u, "err", err)
			continue
		}
		out[t.Meta.ID] = t
	}
	return out, nil
}

// List returns summaries filtered by a case-insensitive query and/or category, sorted by title.
func (c *Catalog) List(ctx context.Context, q, category, source string) []Summary {
	q = strings.ToLower(strings.TrimSpace(q))
	out := []Summary{}
	for _, t := range c.all(ctx) {
		m := t.Meta
		if source != "" && !(source == BuiltinSource && t.Source == "") && t.Source != source {
			continue
		}
		if category != "" && !strings.EqualFold(m.Category, category) {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(m.Title+" "+m.Tagline+" "+m.ID), q) {
			continue
		}
		out = append(out, Summary{ID: m.ID, Title: m.Title, Tagline: m.Tagline, Icon: m.Icon, Category: m.Category, Source: t.Source, Unsupported: t.Unsupported != ""})
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Title) < strings.ToLower(out[j].Title) })
	return out
}

// Get returns one template.
func (c *Catalog) Get(ctx context.Context, id string) (*Template, error) {
	if t, ok := c.all(ctx)[id]; ok {
		return t, nil
	}
	return nil, ErrNotFound
}

// ReservedVars are filled by the system and cannot be used as template field keys.
var ReservedVars = map[string]bool{"APPDATA": true, "PUID": true, "PGID": true, "TZ": true, "PORT": true}

// BuiltinSource is the pseudo source id used to filter the embedded catalog.
const BuiltinSource = "builtin"
