package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"omvhome/internal/apps"
	"omvhome/internal/catalog"
	"omvhome/internal/compose"
	"omvhome/internal/docker"
)

type harness struct {
	inst    *Installer
	dm      *docker.Mock
	root    string
	mkdirs  []string
	portsIn map[int]bool
}

// failingCompose wraps the mock runner so Up can be made to fail.
type failingCompose struct {
	compose.Runner
	failUp bool
	mu     sync.Mutex
	downs  int
}

func (f *failingCompose) Up(ctx context.Context, p, d string, on func(string)) error {
	if f.failUp {
		on("Pulling x")
		return errors.New("manifest unknown")
	}
	return f.Runner.Up(ctx, p, d, on)
}
func (f *failingCompose) Down(ctx context.Context, p, d string) error {
	f.mu.Lock()
	f.downs++
	f.mu.Unlock()
	return f.Runner.Down(ctx, p, d)
}

func newHarness(t *testing.T) (*harness, *failingCompose) {
	t.Helper()
	dm := docker.NewMock()
	fc := &failingCompose{Runner: compose.NewMockRunner(dm)}
	root := t.TempDir()
	appSvc := &apps.Service{Docker: dm, Compose: fc, DataDir: filepath.Join(root, "data"), AppDataDir: filepath.Join(root, "appdata")}
	cat, err := catalog.New("")
	if err != nil {
		t.Fatal(err)
	}
	h := &harness{dm: dm, root: root, portsIn: map[int]bool{}}
	h.inst = NewInstaller(cat, appSvc, dm, fc, "1000", "100", "America/Sao_Paulo")
	h.inst.PortBusy = func(p int) bool { return h.portsIn[p] }
	h.inst.MkdirAll = func(p string) error { h.mkdirs = append(h.mkdirs, p); return nil }
	return h, fc
}

func wait(t *testing.T, j *Job) []Event {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var evs []Event
	if err := j.Stream(ctx, func(e Event) error { evs = append(evs, e); return nil }); err != nil {
		t.Fatalf("stream: %v (events so far: %v)", err, evs)
	}
	return evs
}

func TestInstallRendersFilesAndStarts(t *testing.T) { // A05.2
	h, _ := newHarness(t)
	job, err := h.inst.Install(context.Background(), "qbittorrent", InstallRequest{Env: map[string]string{"DOWNLOADS_DIR": "/srv/dev-disk/downloads"}, Port: 9096})
	if err != nil {
		t.Fatal(err)
	}
	evs := wait(t, job)
	if evs[0].Type != "starting" || evs[len(evs)-1].Type != "done" {
		t.Fatalf("events: %v", evs)
	}

	dir := h.inst.Apps.ProjectDir("qbittorrent")
	env, _ := os.ReadFile(filepath.Join(dir, compose.EnvFile))
	for _, want := range []string{"PORT=9096", "PUID=1000", "PGID=100", "TZ=America/Sao_Paulo", "DOWNLOADS_DIR=/srv/dev-disk/downloads", "APPDATA="} {
		if !strings.Contains(string(env), want) {
			t.Errorf(".env missing %q:\n%s", want, env)
		}
	}
	if st, _ := os.Stat(filepath.Join(dir, compose.EnvFile)); st.Mode().Perm()&0o077 != 0 && os.PathSeparator == '/' {
		t.Errorf(".env must not be group/world readable: %v", st.Mode())
	}
	yml, _ := os.ReadFile(filepath.Join(dir, compose.ComposeFile))
	if !strings.Contains(string(yml), "omvhome.app: qbittorrent") || !strings.Contains(string(yml), `omvhome.port: "9096"`) {
		t.Errorf("labels not injected:\n%s", yml)
	}
	if strings.Contains(string(yml), "x-omvhome") {
		t.Error("x-omvhome extension should be stripped from the rendered file")
	}
	if got := strings.Join(h.mkdirs, "|"); !strings.Contains(got, "/srv/dev-disk/downloads") || !strings.Contains(got, "appdata") {
		t.Errorf("data folders: %v", h.mkdirs)
	}

	// the app shows up as a managed, running app on the chosen port
	list, _ := h.inst.Apps.List(context.Background())
	var found *apps.App
	for i := range list {
		if list[i].ID == "qbittorrent" {
			found = &list[i]
		}
	}
	if found == nil || !found.Managed || found.State != "running" || found.Port != 9096 {
		t.Fatalf("installed app: %+v", found)
	}
}

func TestLabelsOnlyOnMainService(t *testing.T) { // A05.2
	raw := "services:\n  web:\n    image: a\n    labels:\n      - keep=1\n  db:\n    image: b\n"
	out, err := injectLabels(raw, "web", map[string]string{"omvhome.app": "x"})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, "keep:") {
		t.Errorf("existing labels lost:\n%s", s)
	}
	if strings.Count(s, "omvhome.app") != 1 {
		t.Errorf("label must appear once (main only):\n%s", s)
	}
	if _, err := injectLabels(raw, "ghost", nil); err == nil {
		t.Error("unknown main service must fail")
	}
}

func TestDuplicateInstallIsConflict(t *testing.T) { // A05.3
	h, _ := newHarness(t)
	// the Docker mock already seeds an app called "pihole"
	_, err := h.inst.Install(context.Background(), "pihole", InstallRequest{Env: map[string]string{"WEBPASSWORD": "x"}})
	var ce *ConflictError
	if !errors.As(err, &ce) {
		t.Fatalf("want ConflictError, got %v", err)
	}

	job, err := h.inst.Install(context.Background(), "vaultwarden", InstallRequest{})
	if err != nil {
		t.Fatal(err)
	}
	wait(t, job)
	if _, err := h.inst.Install(context.Background(), "vaultwarden", InstallRequest{}); !errors.As(err, &ce) {
		t.Fatalf("second install: want ConflictError, got %v", err)
	}
}

func TestPortConflictBeforeAnyWrite(t *testing.T) { // A05.4
	h, _ := newHarness(t)
	// container conflict: the seeded jellyfin container publishes 8096
	_, err := h.inst.Install(context.Background(), "uptime-kuma", InstallRequest{Port: 8096})
	var ce *ConflictError
	if !errors.As(err, &ce) || !strings.Contains(ce.Msg, "8096") || !strings.Contains(ce.Msg, "jellyfin") {
		t.Fatalf("container conflict: %v", err)
	}
	// host conflict
	h.portsIn[3001] = true
	_, err = h.inst.Install(context.Background(), "uptime-kuma", InstallRequest{})
	if !errors.As(err, &ce) || !strings.Contains(ce.Msg, "3001") {
		t.Fatalf("host conflict: %v", err)
	}
	if _, statErr := os.Stat(h.inst.Apps.ProjectDir("uptime-kuma")); !os.IsNotExist(statErr) {
		t.Error("nothing may be written to disk when the port check fails")
	}
	if len(h.mkdirs) != 0 {
		t.Errorf("no data folder may be created: %v", h.mkdirs)
	}
}

func TestInvalidFieldValues(t *testing.T) { // A05.5
	h, _ := newHarness(t)
	bad := []map[string]string{
		{"MEDIA_DIR": "/srv/../etc"},
		{"MEDIA_DIR": "relative/path"},
		{"MEDIA_DIR": ""}, // required, default is overridden by explicit empty
		{"MEDIA_DIR": "/srv/x\nEVIL=1"},
	}
	for _, env := range bad {
		_, err := h.inst.Install(context.Background(), "jellyfin", InstallRequest{Env: env})
		var ie *InvalidError
		if !errors.As(err, &ie) {
			t.Errorf("%v: want InvalidError, got %v", env, err)
		}
	}
	if _, err := h.inst.Install(context.Background(), "nextcloud", InstallRequest{}); err == nil || !strings.Contains(err.Error(), "obrigatório") {
		t.Errorf("required password: %v", err)
	}
	if _, err := os.Stat(h.inst.Apps.ProjectDir("jellyfin")); !os.IsNotExist(err) {
		t.Error("project dir must not exist after validation failures")
	}
	if _, err := h.inst.Install(context.Background(), "nope", InstallRequest{}); !errors.Is(err, catalog.ErrNotFound) {
		t.Errorf("unknown template: %v", err)
	}
}

func TestCustomRejectsUnsafeWithoutAllow(t *testing.T) { // A05.6
	h, _ := newHarness(t)
	base := "services:\n  app:\n    image: nginx\n"
	cases := map[string]string{
		"privileged":  base + "    privileged: true\n",
		"host net":    base + "    network_mode: host\n",
		"docker sock": base + "    volumes:\n      - /var/run/docker.sock:/var/run/docker.sock\n",
		"pid host":    base + "    pid: host\n",
	}
	for name, yml := range cases {
		_, err := h.inst.InstallCustom(context.Background(), CustomRequest{Name: "My App", Compose: yml})
		var ie *InvalidError
		if !errors.As(err, &ie) || !strings.Contains(ie.Msg, "allowUnsafe") {
			t.Errorf("%s: want InvalidError mentioning allowUnsafe, got %v", name, err)
		}
	}
	job, err := h.inst.InstallCustom(context.Background(), CustomRequest{Name: "My App", Compose: cases["host net"], AllowUnsafe: true})
	if err != nil {
		t.Fatalf("allowUnsafe: %v", err)
	}
	wait(t, job)
	if _, err := os.Stat(filepath.Join(h.inst.Apps.ProjectDir("my-app"), compose.ComposeFile)); err != nil {
		t.Error(err)
	}
}

func TestCustomValidation(t *testing.T) {
	h, _ := newHarness(t)
	two := "services:\n  a:\n    image: x\n  b:\n    image: y\n"
	for name, req := range map[string]CustomRequest{
		"not yaml":       {Name: "x1", Compose: ":\n\t["},
		"no services":    {Name: "x2", Compose: "version: '3'\n"},
		"bad name":       {Name: "!!!", Compose: two},
		"main required":  {Name: "x3", Compose: two},
		"main not found": {Name: "x4", Compose: two, MainService: "zzz"},
	} {
		if _, err := h.inst.InstallCustom(context.Background(), req); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
	job, err := h.inst.InstallCustom(context.Background(), CustomRequest{Name: "Two Svc", Compose: two, MainService: "b"})
	if err != nil {
		t.Fatal(err)
	}
	wait(t, job)
	yml, _ := os.ReadFile(filepath.Join(h.inst.Apps.ProjectDir("two-svc"), compose.ComposeFile))
	if strings.Count(string(yml), "omvhome.app") != 1 {
		t.Errorf("labels:\n%s", yml)
	}
}

func TestFailedInstallRollsBack(t *testing.T) {
	h, fc := newHarness(t)
	fc.failUp = true
	job, err := h.inst.Install(context.Background(), "uptime-kuma", InstallRequest{})
	if err != nil {
		t.Fatal(err)
	}
	evs := wait(t, job)
	last := evs[len(evs)-1]
	if last.Type != "error" || !strings.Contains(last.Message, "manifest unknown") {
		t.Fatalf("events: %v", evs)
	}
	if _, err := os.Stat(h.inst.Apps.ProjectDir("uptime-kuma")); !os.IsNotExist(err) {
		t.Error("project dir should be removed after a failed install")
	}
	if fc.downs != 1 {
		t.Errorf("compose down calls: %d", fc.downs)
	}
	fc.failUp = false // retry works, no 409 left behind
	job, err = h.inst.Install(context.Background(), "uptime-kuma", InstallRequest{})
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	wait(t, job)
}

func TestComposeUnavailable(t *testing.T) {
	h, _ := newHarness(t)
	h.inst.Compose = unavailable{}
	if _, err := h.inst.Install(context.Background(), "uptime-kuma", InstallRequest{}); !errors.Is(err, compose.ErrUnavailable) {
		t.Fatalf("got %v", err)
	}
}

type unavailable struct{ compose.Runner }

func (unavailable) Available(context.Context) bool { return false }

func TestJobReplaysForLateSubscribers(t *testing.T) {
	j := newJob("x")
	j.emit(Event{Type: "starting"})
	j.emit(Event{Type: "log", Message: "a"})
	j.emit(Event{Type: "done"})
	evs := wait(t, j)
	if len(evs) != 3 || !j.Done() {
		t.Fatalf("%v", evs)
	}
	// cancelled subscriber returns promptly
	j2 := newJob("y")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := j2.Stream(ctx, func(Event) error { return nil }); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
}

func TestDotenvQuote(t *testing.T) {
	cases := map[string]string{
		"simple":     "simple",
		"/srv/media": "/srv/media",
		"has space":  "'has space'",
		"pa$$word":   "'pa$$word'",
		"it's $x":    `"it's \$x"`,
		`back\slash`: `'back\slash'`,
		"":           "''",
		"a#b":        "'a#b'",
	}
	for in, want := range cases {
		if got := dotenvQuote(in); got != want {
			t.Errorf("%q: got %s want %s", in, got, want)
		}
	}
}

func TestSlugify(t *testing.T) {
	cases := map[string]string{"My App": "my-app", "  Nextcloud 2! ": "nextcloud-2", "ÄÖ": "", "a__b": "a-b"}
	for in, want := range cases {
		if got := slugify(in); got != want {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
}
