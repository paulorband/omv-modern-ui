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

func TestAppDataOwners(t *testing.T) { // A09.9
	app := filepath.Join(t.TempDir(), "x")
	yml := `services:
  web:
    image: a
    user: "1000:1000"
    volumes:
      - ${APPDATA}/data:/data
      - $APPDATA/cache:/cache:ro
      - ${APPDATA}/conf.json:/conf.json
      - /srv/media:/media
      - ${APPDATA}/../escape:/e
      - type: bind
        source: ${APPDATA}/maps
        target: /maps
      - named:/vol
  db:
    image: b
    user: 999
    volumes:
      - ${APPDATA}/db:/var/lib/db
      - ${APPDATA}/data:/dup
  proxy:
    image: c
    user: nginx
    volumes:
      - ${APPDATA}/proxy:/p
  plain:
    image: d
    volumes:
      - ${APPDATA}/plain:/p
`
	got := map[string][2]int{}
	for _, o := range appDataOwners([]byte(yml), app) {
		got[o.path] = [2]int{o.uid, o.gid}
	}
	j := func(p string) string { return filepath.Join(app, p) }
	want := map[string][2]int{
		j("cache"): {1000, 1000}, j("conf.json"): {1000, 1000}, j("data"): {999, -1}, j("db"): {999, -1}, j("maps"): {1000, 1000},
	}
	// "data" is claimed by both web and db: the first service by name (db) wins.
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for p, w := range want {
		if got[p] != w {
			t.Errorf("%s: got %v want %v", p, got[p], w)
		}
	}
}

func TestParseUser(t *testing.T) {
	for in, want := range map[any][3]int{"1000": {1000, -1, 1}, "1000:100": {1000, 100, 1}, 33: {33, -1, 1}, "1000:users": {1000, -1, 1}, "www-data": {0, 0, 0}, "": {0, 0, 0}, nil: {0, 0, 0}, "-1": {0, 0, 0}} {
		uid, gid, ok := parseUser(in)
		if !ok && want[2] == 1 || ok && (want[2] == 0 || uid != want[0] || gid != want[1]) {
			t.Errorf("%v → %d %d %v", in, uid, gid, ok)
		}
	}
}

func TestInstallHandsDataFoldersToContainerUser(t *testing.T) { // A09.9
	h, _ := newHarness(t)
	type chown struct {
		path     string
		uid, gid int
	}
	var chowns []chown
	h.inst.Chown = func(p string, uid, gid int) error { chowns = append(chowns, chown{p, uid, gid}); return nil }
	// make the folders real so the recursive walk has something to visit
	h.inst.MkdirAll = func(p string) error { h.mkdirs = append(h.mkdirs, p); return os.MkdirAll(p, 0o755) }

	yml := "services:\n  app:\n    image: nginx\n    user: '1000:1000'\n    volumes:\n      - ${APPDATA}/data/nested:/data\n"
	job, err := h.inst.InstallCustom(context.Background(), CustomRequest{Name: "Owned App", Compose: yml})
	if err != nil {
		t.Fatal(err)
	}
	wait(t, job)

	data := filepath.Join(h.inst.Apps.AppDataDir, "owned-app", "data", "nested")
	found := false
	for _, m := range h.mkdirs {
		found = found || m == data
	}
	if !found {
		t.Errorf("data folder not pre-created (Docker would create it as root): %v", h.mkdirs)
	}
	if len(chowns) == 0 || chowns[0] != (chown{data, 1000, 1000}) {
		t.Errorf("chowns: %+v", chowns)
	}
}

func TestInstallWithoutNumericUserDoesNotChown(t *testing.T) {
	h, _ := newHarness(t)
	called := false
	h.inst.Chown = func(string, int, int) error { called = true; return nil }
	yml := "services:\n  app:\n    image: nginx\n    volumes:\n      - ${APPDATA}/data:/data\n"
	job, err := h.inst.InstallCustom(context.Background(), CustomRequest{Name: "Plain App", Compose: yml})
	if err != nil {
		t.Fatal(err)
	}
	wait(t, job)
	if called {
		t.Error("built-in style apps (no numeric user) must keep their current ownership behaviour")
	}
}

func TestChownFailureOnlyWarns(t *testing.T) {
	h, _ := newHarness(t)
	h.inst.Chown = func(string, int, int) error { return os.ErrPermission }
	h.inst.MkdirAll = func(p string) error { return os.MkdirAll(p, 0o755) }
	yml := "services:\n  app:\n    image: nginx\n    user: '1000'\n    volumes:\n      - ${APPDATA}/data:/data\n"
	job, err := h.inst.InstallCustom(context.Background(), CustomRequest{Name: "Warn App", Compose: yml})
	if err != nil {
		t.Fatalf("a chown failure must not block the install: %v", err)
	}
	wait(t, job)
}

// gatedCompose blocks Up per project until released, so tests can observe running installs.
type gatedCompose struct {
	compose.Runner
	gates map[string]chan struct{} // project → release
	fail  map[string]bool
}

func (g *gatedCompose) Up(ctx context.Context, p, d string, on func(string)) error {
	on("Pulling " + p)
	if gate, ok := g.gates[p]; ok {
		select {
		case <-gate:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if g.fail[p] {
		on("Error response from daemon: unauthorized")
		return errors.New("pull access denied")
	}
	return g.Runner.Up(ctx, p, d, on)
}

func waitState(t *testing.T, i *Installer, id, state string) JobSummary {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, s := range i.Jobs() {
			if s.ID == id && s.State == state {
				return s
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("job %s never reached %s: %+v", id, state, i.Jobs())
	return JobSummary{}
}

func TestJobsListSeveralConcurrentInstalls(t *testing.T) { // A10.1, A10.2, A10.3
	h, _ := newHarness(t)
	g := &gatedCompose{Runner: compose.NewMockRunner(h.dm), gates: map[string]chan struct{}{}, fail: map[string]bool{}}
	h.inst.Compose = g
	for _, id := range []string{"uptime-kuma", "vaultwarden"} {
		g.gates[compose.ProjectName(id)] = make(chan struct{})
	}
	g.fail[compose.ProjectName("vaultwarden")] = true

	for _, id := range []string{"uptime-kuma", "vaultwarden"} {
		if _, err := h.inst.Install(context.Background(), id, InstallRequest{}); err != nil {
			t.Fatal(err)
		}
	}
	waitState(t, h.inst, "uptime-kuma", "running")
	waitState(t, h.inst, "vaultwarden", "running")
	var list []JobSummary
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		if list = h.inst.Jobs(); len(list) == 2 && list[0].Message != "" && list[1].Message != "" {
			break
		}
	}
	if len(list) != 2 || list[0].Title == "" || list[0].Message == "" || list[1].Message == "" {
		t.Fatalf("running jobs: %+v", list)
	}
	if err := h.inst.Dismiss("uptime-kuma"); err == nil || !errors.As(err, new(*ConflictError)) {
		t.Fatalf("dismissing a running job: %v", err)
	}

	close(g.gates[compose.ProjectName("vaultwarden")]) // this one fails
	failed := waitState(t, h.inst, "vaultwarden", "error")
	if failed.FinishedAt == nil || !strings.Contains(failed.Message, "pull access denied") {
		t.Fatalf("failed summary: %+v", failed)
	}
	if got := waitState(t, h.inst, "uptime-kuma", "running"); got.FinishedAt != nil {
		t.Fatalf("the other install must stay running: %+v", got)
	}

	close(g.gates[compose.ProjectName("uptime-kuma")])
	waitState(t, h.inst, "uptime-kuma", "done")

	// the full log of the failed install is still there (A10.2)
	job, ok := h.inst.Job("vaultwarden")
	if !ok {
		t.Fatal("failed job vanished")
	}
	var log []string
	for _, e := range wait(t, job) {
		log = append(log, e.Type+":"+e.Message)
	}
	joined := strings.Join(log, "\n")
	for _, want := range []string{"starting:", "pull:Pulling", "log:Error response from daemon", "error:pull access denied"} {
		if !strings.Contains(joined, want) {
			t.Errorf("log missing %q:\n%s", want, joined)
		}
	}

	// dismissing (A10.3)
	if err := h.inst.Dismiss("vaultwarden"); err != nil {
		t.Fatal(err)
	}
	if err := h.inst.Dismiss("vaultwarden"); !errors.Is(err, ErrJobNotFound) {
		t.Fatalf("dismiss twice: %v", err)
	}
	if len(h.inst.Jobs()) != 1 {
		t.Fatalf("after dismiss: %+v", h.inst.Jobs())
	}
}

func TestFinishedJobsExpireAfterRetention(t *testing.T) { // A10.4
	h, _ := newHarness(t)
	old := newJob("old")
	old.emit(Event{Type: "done"})
	old.finishedAt = time.Now().Add(-jobRetention - time.Minute)
	fresh := newJob("fresh")
	fresh.emit(Event{Type: "done"})
	h.inst.jobs["old"], h.inst.jobs["fresh"] = old, fresh
	list := h.inst.Jobs()
	if len(list) != 1 || list[0].ID != "fresh" {
		t.Fatalf("%+v", list)
	}
}

func TestJobLogIsBoundedButKeepsTheEnd(t *testing.T) { // R10.5
	j := newJob("big")
	for i := 0; i < maxLogEvents+500; i++ {
		j.emit(Event{Type: "log", Message: "line"})
	}
	j.emit(Event{Type: "error", Message: "boom"})
	evs := wait(t, j)
	if len(evs) != maxLogEvents+1 || evs[len(evs)-1].Type != "error" {
		t.Fatalf("len=%d last=%+v", len(evs), evs[len(evs)-1])
	}
	if s := j.Summary(); s.State != "error" || s.Message != "boom" {
		t.Fatalf("%+v", s)
	}
}

func TestSummaryStates(t *testing.T) {
	j := newJob("a")
	if s := j.Summary(); s.State != "running" || s.Title != "a" || s.FinishedAt != nil {
		t.Fatalf("%+v", s)
	}
	j.emit(Event{Type: "done", Message: "ok"})
	if s := j.Summary(); s.State != "done" || s.Message != "ok" || s.FinishedAt == nil {
		t.Fatalf("%+v", s)
	}
}

func TestInternalPortFieldsAreNotHostPorts(t *testing.T) {
	m := catalog.Meta{Env: []catalog.EnvField{
		{Key: "CONTAINER_PORT", Label: "Porta interna", Type: "port", Default: "9000", Internal: true},
		{Key: "OTHER", Label: "Outra", Type: "port", Default: "9001"},
	}}
	_, _, ports, err := resolveFields(m, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(ports) != 1 || ports[0] != 9001 {
		t.Fatalf("only host ports are checked for conflicts: %v", ports)
	}
	if _, _, _, err := resolveFields(m, map[string]string{"CONTAINER_PORT": "0"}); err == nil {
		t.Fatal("internal ports are still validated")
	}
}

func TestQbittorrentUsesSamePortOnBothSides(t *testing.T) {
	h, _ := newHarness(t)
	job, err := h.inst.Install(context.Background(), "qbittorrent", InstallRequest{Port: 8099, Env: map[string]string{"DOWNLOADS_DIR": "/srv/dl"}})
	if err != nil {
		t.Fatal(err)
	}
	wait(t, job)
	env, _ := os.ReadFile(filepath.Join(h.inst.Apps.ProjectDir("qbittorrent"), compose.EnvFile))
	yml, _ := os.ReadFile(filepath.Join(h.inst.Apps.ProjectDir("qbittorrent"), compose.ComposeFile))
	if !strings.Contains(string(env), "PORT=8099") || !strings.Contains(string(yml), `${PORT:-8083}:${PORT:-8083}`) || !strings.Contains(string(yml), `WEBUI_PORT: ${PORT:-8083}`) {
		t.Fatalf("env:\n%s\ncompose:\n%s", env, yml)
	}
}
