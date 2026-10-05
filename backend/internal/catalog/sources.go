package catalog

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Custom app sources: third-party stores in ZimaOS/CasaOS or Umbrel format (SPEC-09).

const (
	FormatUmbrel = "umbrel"
	FormatCasaos = "casaos"

	maxZipBytes      = 100 << 20
	maxFileBytes     = 1 << 20
	maxAppsPerSource = 3000
)

var (
	ErrSourceNotFound = errors.New("fonte não encontrada")

	sourceIDRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)
	githubRe   = regexp.MustCompile(`^/([A-Za-z0-9_.-]+)/([A-Za-z0-9_.-]+?)(?:\.git)?(?:/tree/(.+?))?/?$`)
)

// SourceInvalidError is bad input (HTTP 400); SourceConflictError a duplicate (409);
// SourceFetchError a download failure (502).
type (
	SourceInvalidError  struct{ Msg string }
	SourceConflictError struct{ Msg string }
	SourceFetchError    struct{ Err error }
)

func (e *SourceInvalidError) Error() string  { return e.Msg }
func (e *SourceConflictError) Error() string { return e.Msg }
func (e *SourceFetchError) Error() string {
	return "não foi possível baixar a fonte: " + e.Err.Error()
}
func (e *SourceFetchError) Unwrap() error { return e.Err }

func invalidSource(format string, a ...any) error {
	return &SourceInvalidError{fmt.Sprintf(format, a...)}
}

// Source describes one configured custom source.
type Source struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	URL       string    `json:"url"`
	Format    string    `json:"format"`
	Apps      int       `json:"apps"`
	UpdatedAt time.Time `json:"updatedAt"`
	Error     string    `json:"error,omitempty"`
}

type sourceState struct {
	Source
	apps map[string]*Template // by base id (before clash resolution)
}

// UseDir enables custom sources persisted under dir/sources and loads the cached ones (no network).
func (c *Catalog) UseDir(dir string) error {
	c.sdir = filepath.Join(dir, "sources")
	if err := os.MkdirAll(c.sdir, 0o750); err != nil {
		return err
	}
	raw, err := os.ReadFile(filepath.Join(c.sdir, "sources.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var list []Source
	if err := json.Unmarshal(raw, &list); err != nil {
		return fmt.Errorf("sources.json inválido: %w", err)
	}
	c.smu.Lock()
	defer c.smu.Unlock()
	for _, s := range list {
		st := &sourceState{Source: s, apps: map[string]*Template{}}
		if zipRaw, err := os.ReadFile(c.zipPath(s.ID)); err == nil {
			if _, _, apps, err := loadRepo(zipRaw, s.ID); err == nil {
				st.apps = apps
			} else {
				st.Error = err.Error()
			}
		} else {
			st.Error = "cache da fonte ausente; atualize-a"
		}
		st.Apps = len(st.apps)
		c.sources = append(c.sources, st)
	}
	c.rebuildLocked()
	return nil
}

func (c *Catalog) zipPath(id string) string { return filepath.Join(c.sdir, id+".zip") }

func (c *Catalog) persistLocked() error {
	list := make([]Source, 0, len(c.sources))
	for _, s := range c.sources {
		list = append(list, s.Source)
	}
	raw, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(c.sdir, "sources.json"), raw)
}

func writeFileAtomic(p string, data []byte) error {
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o640); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// Sources lists configured sources in the order they were added.
func (c *Catalog) Sources() []Source {
	c.smu.RLock()
	defer c.smu.RUnlock()
	out := make([]Source, 0, len(c.sources))
	for _, s := range c.sources {
		out = append(out, s.Source)
	}
	return out
}

// AddSource downloads, converts and persists a new source (R09.2).
func (c *Catalog) AddSource(ctx context.Context, rawURL, name string) (Source, error) {
	if c.sdir == "" {
		return Source{}, errors.New("fontes personalizadas desativadas")
	}
	dl, err := downloadURL(rawURL)
	if err != nil {
		return Source{}, err
	}
	c.smu.RLock()
	for _, s := range c.sources {
		if s.URL == dl.canonical {
			c.smu.RUnlock()
			return Source{}, &SourceConflictError{"esta fonte já foi adicionada"}
		}
	}
	c.smu.RUnlock()

	zipRaw, err := c.fetchZip(ctx, dl.zipURL)
	if err != nil {
		return Source{}, &SourceFetchError{err}
	}
	format, storeName, apps, err := loadRepo(zipRaw, "")
	if err != nil {
		return Source{}, err
	}

	c.smu.Lock()
	defer c.smu.Unlock()
	id := c.uniqueSourceIDLocked(firstNonEmpty(dl.slug, slugify(storeName)))
	if err := writeFileAtomic(c.zipPath(id), zipRaw); err != nil {
		return Source{}, err
	}
	st := &sourceState{
		Source: Source{ID: id, Name: firstNonEmpty(name, storeName, id), URL: dl.canonical, Format: format, Apps: len(apps), UpdatedAt: time.Now().UTC()},
		apps:   apps,
	}
	c.sources = append(c.sources, st)
	if err := c.persistLocked(); err != nil {
		c.sources = c.sources[:len(c.sources)-1]
		_ = os.Remove(c.zipPath(id))
		return Source{}, err
	}
	c.rebuildLocked()
	return st.Source, nil
}

// RemoveSource deletes a source and its cache; installed apps are unaffected (R09.3).
func (c *Catalog) RemoveSource(id string) error {
	c.smu.Lock()
	defer c.smu.Unlock()
	for i, s := range c.sources {
		if s.ID != id {
			continue
		}
		c.sources = append(c.sources[:i:i], c.sources[i+1:]...)
		if err := c.persistLocked(); err != nil {
			return err
		}
		_ = os.Remove(c.zipPath(id))
		c.rebuildLocked()
		return nil
	}
	return ErrSourceNotFound
}

// RefreshSource downloads a source again; on failure the previous cache stays in use (R09.4).
func (c *Catalog) RefreshSource(ctx context.Context, id string) (Source, error) {
	c.smu.RLock()
	var cur *sourceState
	for _, s := range c.sources {
		if s.ID == id {
			cur = s
		}
	}
	var srcURL string
	if cur != nil {
		srcURL = cur.URL
	}
	c.smu.RUnlock()
	if cur == nil {
		return Source{}, ErrSourceNotFound
	}
	dl, err := downloadURL(srcURL)
	if err != nil {
		return Source{}, err
	}
	zipRaw, err := c.fetchZip(ctx, dl.zipURL)
	if err == nil {
		var format string
		var apps map[string]*Template
		format, _, apps, err = loadRepo(zipRaw, id)
		if err == nil {
			c.smu.Lock()
			defer c.smu.Unlock()
			if werr := writeFileAtomic(c.zipPath(id), zipRaw); werr != nil {
				return Source{}, werr
			}
			cur.apps, cur.Format, cur.Apps, cur.UpdatedAt, cur.Error = apps, format, len(apps), time.Now().UTC(), ""
			if perr := c.persistLocked(); perr != nil {
				return Source{}, perr
			}
			c.rebuildLocked()
			return cur.Source, nil
		}
		c.recordSourceError(cur, err)
		return Source{}, err
	}
	c.recordSourceError(cur, err)
	return Source{}, &SourceFetchError{err}
}

func (c *Catalog) recordSourceError(s *sourceState, err error) {
	c.smu.Lock()
	defer c.smu.Unlock()
	s.Error = err.Error()
	_ = c.persistLocked()
}

// RefreshStale refreshes sources older than maxAge; failures are only logged (R09.8).
func (c *Catalog) RefreshStale(ctx context.Context, maxAge time.Duration) {
	for _, s := range c.Sources() {
		if time.Since(s.UpdatedAt) < maxAge {
			continue
		}
		if _, err := c.RefreshSource(ctx, s.ID); err != nil {
			slog.Warn("não foi possível atualizar a fonte", "source", s.ID, "err", err)
		}
	}
}

func (c *Catalog) uniqueSourceIDLocked(base string) string {
	if !sourceIDRe.MatchString(base) || base == BuiltinSource {
		base = "fonte"
	}
	id := base
	for n := 2; c.hasSourceLocked(id) || id == BuiltinSource; n++ {
		suffix := fmt.Sprintf("-%d", n)
		id = strings.TrimRight(base[:min(len(base), 40-len(suffix))], "-") + suffix
	}
	return id
}

func (c *Catalog) hasSourceLocked(id string) bool {
	for _, s := range c.sources {
		if s.ID == id {
			return true
		}
	}
	return false
}

// rebuildLocked resolves id clashes (R09.7): built-in apps win; between sources the newer one gets
// a "-<source>" suffix.
func (c *Catalog) rebuildLocked() {
	merged := map[string]*Template{}
	taken := func(id string) bool {
		if _, ok := c.embedded[id]; ok {
			return true
		}
		_, ok := merged[id]
		return ok
	}
	for _, s := range c.sources {
		for _, baseID := range sortedKeys(s.apps) {
			t := s.apps[baseID]
			id := baseID
			if taken(id) {
				suffix := "-" + s.ID
				id = strings.TrimRight(baseID[:min(len(baseID), 40-len(suffix))], "-") + suffix
				if taken(id) {
					continue
				}
			}
			cp := *t
			cp.Meta.ID = id
			cp.Source = s.ID
			merged[id] = &cp
		}
	}
	c.sourceApps = merged
}

func (c *Catalog) sourceTemplates() map[string]*Template {
	c.smu.RLock()
	defer c.smu.RUnlock()
	out := make(map[string]*Template, len(c.sourceApps))
	for id, t := range c.sourceApps {
		out[id] = t
	}
	return out
}

// SourceName returns the display name of a source id ("" if unknown).
func (c *Catalog) SourceName(id string) string {
	c.smu.RLock()
	defer c.smu.RUnlock()
	for _, s := range c.sources {
		if s.ID == id {
			return s.Name
		}
	}
	return ""
}

// ---- download ----

type download struct {
	canonical string // normalized URL stored in the source
	zipURL    string
	slug      string // suggested source id
}

// downloadURL turns a repository or zip URL into a zip download (see SPEC-09).
func downloadURL(raw string) (download, error) {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return download{}, invalidSource("informe uma URL http(s) de repositório do GitHub ou de um arquivo .zip")
	}
	u.Fragment, u.RawQuery = "", ""
	if strings.EqualFold(u.Hostname(), "github.com") {
		m := githubRe.FindStringSubmatch(u.Path)
		if m == nil {
			return download{}, invalidSource("URL do GitHub inválida: use https://github.com/<dono>/<repo>")
		}
		owner, repo, branch := m[1], m[2], m[3]
		canonical := "https://github.com/" + owner + "/" + repo
		zipURL := canonical + "/archive/HEAD.zip"
		if branch != "" {
			canonical += "/tree/" + branch
			zipURL = "https://github.com/" + owner + "/" + repo + "/archive/refs/heads/" + branch + ".zip"
		}
		return download{canonical: canonical, zipURL: zipURL, slug: slugify(repo)}, nil
	}
	if strings.HasSuffix(strings.ToLower(u.Path), ".zip") {
		return download{canonical: u.String(), zipURL: u.String(), slug: slugify(strings.TrimSuffix(path.Base(u.Path), path.Ext(u.Path)))}, nil
	}
	return download{}, invalidSource("URL não reconhecida: use um repositório do GitHub ou um link para um arquivo .zip")
}

func (c *Catalog) fetchZip(ctx context.Context, zipURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, zipURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.zipClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxZipBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxZipBytes {
		return nil, fmt.Errorf("arquivo maior que %d MB", maxZipBytes>>20)
	}
	return data, nil
}

// ---- repository parsing ----

type repoFiles struct {
	files map[string]*zip.File // path without the top-level folder
}

func openRepo(data []byte) (*repoFiles, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, invalidSource("o arquivo baixado não é um zip válido")
	}
	top := ""
	single := true
	for _, f := range zr.File {
		first, _, _ := strings.Cut(f.Name, "/")
		if top == "" {
			top = first
		} else if first != top {
			single = false
			break
		}
	}
	rf := &repoFiles{files: map[string]*zip.File{}}
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		name := f.Name
		if single {
			_, name, _ = strings.Cut(name, "/")
		}
		rf.files[name] = f
	}
	return rf, nil
}

func (r *repoFiles) read(name string) ([]byte, error) {
	f, ok := r.files[name]
	if !ok {
		return nil, os.ErrNotExist
	}
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, maxFileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxFileBytes {
		return nil, fmt.Errorf("%s: arquivo grande demais", name)
	}
	return data, nil
}

// loadRepo detects the format of a repository zip and converts its apps.
// It returns the format, the store's own name (Umbrel) and the apps by id.
func loadRepo(data []byte, sourceID string) (format, storeName string, apps map[string]*Template, err error) {
	rf, err := openRepo(data)
	if err != nil {
		return "", "", nil, err
	}
	apps = map[string]*Template{}
	add := func(t *Template, e error, where string) {
		switch {
		case e != nil:
			slog.Debug("app de fonte ignorado", "app", where, "err", e)
		case t != nil && len(apps) < maxAppsPerSource:
			if _, dup := apps[t.Meta.ID]; !dup {
				apps[t.Meta.ID] = t
			}
		}
	}
	names := make([]string, 0, len(rf.files))
	for n := range rf.files {
		names = append(names, n)
	}
	sort.Strings(names)

	if manifest, err := rf.read("umbrel-app-store.yml"); err == nil {
		var sm struct {
			ID   string `yaml:"id"`
			Name string `yaml:"name"`
		}
		_ = yaml.Unmarshal(manifest, &sm)
		storeName = firstNonEmpty(sm.Name, sm.ID)
		for _, n := range names {
			dir, file := path.Split(n)
			dir = strings.TrimSuffix(dir, "/")
			if file != "umbrel-app.yml" || dir == "" || strings.Contains(dir, "/") {
				continue
			}
			mf, e1 := rf.read(n)
			comp, e2 := rf.read(dir + "/docker-compose.yml")
			if e1 != nil || e2 != nil {
				continue
			}
			t, e := convertUmbrel(dir, mf, comp, sourceID)
			add(t, e, dir)
		}
		return FormatUmbrel, storeName, apps, nil
	}

	hasApps := false
	for _, n := range names {
		lower := strings.ToLower(n)
		if !strings.HasPrefix(lower, "apps/") {
			continue
		}
		hasApps = true
		parts := strings.Split(n, "/")
		if len(parts) != 3 || (parts[2] != "docker-compose.yml" && parts[2] != "docker-compose.yaml") {
			continue
		}
		comp, e := rf.read(n)
		if e != nil {
			continue
		}
		t, e := convertCasaos(parts[1], comp, sourceID)
		add(t, e, parts[1])
	}
	if hasApps {
		return FormatCasaos, "", apps, nil
	}
	return "", "", nil, invalidSource("formato não reconhecido: esperado umbrel-app-store.yml (Umbrel) ou a pasta Apps/ (ZimaOS/CasaOS)")
}
