package compose

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"omvhome/internal/docker"
)

func TestExecRunnerBuildsArgListWithoutShell(t *testing.T) { // A05.7
	var gotDir string
	var gotArgs [][]string
	r := &ExecRunner{Exec: func(_ context.Context, dir string, args []string, _ func(string)) error {
		gotDir = dir
		gotArgs = append(gotArgs, args)
		return nil
	}}
	dir := filepath.Join("data", "apps", "jellyfin")
	// A hostile-looking project name stays a single argv element: no shell is involved.
	project := "omvhome-x; rm -rf /"
	_ = r.Up(context.Background(), project, dir, nil)
	_ = r.Stop(context.Background(), project, dir)
	_ = r.Down(context.Background(), project, dir)

	base := []string{"compose", "-p", project, "--project-directory", dir, "-f", filepath.Join(dir, ComposeFile)}
	want := [][]string{
		append(append([]string{}, base...), "up", "-d", "--remove-orphans"),
		append(append([]string{}, base...), "stop"),
		append(append([]string{}, base...), "down", "--remove-orphans"),
	}
	if !reflect.DeepEqual(gotArgs, want) {
		t.Fatalf("args:\n got %q\nwant %q", gotArgs, want)
	}
	if gotDir != dir {
		t.Errorf("dir %q", gotDir)
	}
}

func TestAvailableReflectsExecutor(t *testing.T) {
	ok := &ExecRunner{Exec: func(context.Context, string, []string, func(string)) error { return nil }}
	bad := &ExecRunner{Exec: func(context.Context, string, []string, func(string)) error { return ErrUnavailable }}
	if !ok.Available(context.Background()) || bad.Available(context.Background()) {
		t.Fatal("Available must mirror `docker compose version`")
	}
}

func TestInterpolate(t *testing.T) {
	vars := map[string]string{"APPDATA": "/data/app", "EMPTY": ""}
	cases := map[string]string{
		"${APPDATA}/config:/config": "/data/app/config:/config",
		"${PORT:-8096}:8096":        "8096:8096",
		"${EMPTY:-fallback}":        "fallback",
		"${MISSING}":                "",
		"plain":                     "plain",
	}
	for in, want := range cases {
		if got := Interpolate(in, vars); got != want {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
}

func TestMockRunnerCreatesContainersFromComposeFile(t *testing.T) {
	dir := t.TempDir()
	yml := `services:
  web:
    image: nginx
    ports: ["${PORT:-8080}:80"]
    labels:
      omvhome.app: demo
      omvhome.port: "8080"
  db:
    image: postgres
`
	if err := os.WriteFile(filepath.Join(dir, ComposeFile), []byte(yml), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, EnvFile), []byte("PORT=9090\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dm := docker.NewMock()
	m := NewMockRunner(dm)
	ctx := context.Background()
	if err := m.Up(ctx, "omvhome-demo", dir, nil); err != nil {
		t.Fatal(err)
	}
	if err := m.Up(ctx, "omvhome-demo", dir, nil); err != nil { // idempotent
		t.Fatal(err)
	}
	cs, _ := dm.ListContainers(ctx)
	var proj []docker.Container
	for _, c := range cs {
		if c.Labels[docker.ComposeProject] == "omvhome-demo" {
			proj = append(proj, c)
		}
	}
	if len(proj) != 2 {
		t.Fatalf("want 2 containers, got %d", len(proj))
	}
	for _, c := range proj {
		if c.AppID == "demo" && (len(c.Ports) != 1 || c.Ports[0].Public != 9090) {
			t.Errorf("port from .env not applied: %+v", c.Ports)
		}
	}
	_ = m.Stop(ctx, "omvhome-demo", dir)
	d, _ := dm.InspectContainer(ctx, proj[0].Name)
	if d.State != "exited" {
		t.Errorf("state after stop: %s", d.State)
	}
	_ = m.Down(ctx, "omvhome-demo", dir)
	cs, _ = dm.ListContainers(ctx)
	for _, c := range cs {
		if c.Labels[docker.ComposeProject] == "omvhome-demo" {
			t.Errorf("container survived down: %s", c.Name)
		}
	}
}

func TestSeedDirCopiesImageContentsAndCleansUp(t *testing.T) { // A09.10
	const id = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	var calls [][]string
	r := &ExecRunner{Exec: func(_ context.Context, _ string, args []string, onLine func(string)) error {
		calls = append(calls, args)
		if args[0] == "create" && onLine != nil {
			onLine("Unable to find image 'x' locally")
			onLine(id)
		}
		return nil
	}}
	host := filepath.Join(t.TempDir(), "data")
	if err := r.SeedDir(context.Background(), "gtstef/filebrowser:1", "/home/filebrowser/data/", host); err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"create", "--entrypoint", "true", "--", "gtstef/filebrowser:1"},
		{"cp", "-a", id + ":/home/filebrowser/data/.", host},
		{"rm", "-f", id},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls:\n got %q\nwant %q", calls, want)
	}
}

func TestSeedDirIgnoresMissingPathButNotOtherErrors(t *testing.T) {
	const id = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	run := func(cpErr error) (error, bool) {
		removed := false
		r := &ExecRunner{Exec: func(_ context.Context, _ string, args []string, onLine func(string)) error {
			switch args[0] {
			case "create":
				onLine(id)
			case "cp":
				return cpErr
			case "rm":
				removed = true
			}
			return nil
		}}
		return r.SeedDir(context.Background(), "img", "/data", filepath.Join(t.TempDir(), "d")), removed
	}
	if err, removed := run(errors.New("docker cp falhou: Could not find the file /data in container")); err != nil || !removed {
		t.Errorf("missing path: err=%v removed=%v", err, removed)
	}
	if err, removed := run(errors.New("disco cheio")); err == nil || !removed {
		t.Errorf("real error must surface and still clean up: err=%v removed=%v", err, removed)
	}
}

func TestSeedDirRejectsSuspiciousArguments(t *testing.T) {
	r := &ExecRunner{Exec: func(context.Context, string, []string, func(string)) error {
		t.Fatal("docker must not run")
		return nil
	}}
	abs := filepath.Join(t.TempDir(), "d")
	for _, c := range []struct{ image, path, host string }{
		{"--privileged", "/d", abs}, {"", "/d", abs}, {"img", "relative", abs}, {"img", "/d", "rel/dir"},
	} {
		if err := r.SeedDir(context.Background(), c.image, c.path, c.host); err == nil || !strings.Contains(err.Error(), "inválidos") {
			t.Errorf("%+v: %v", c, err)
		}
	}
}

// imageExec fakes `docker image inspect`, `pull` and `run ... id`.
func imageExec(user string, pulled bool, ids map[string]string) (*ExecRunner, *[]string) {
	var log []string
	return &ExecRunner{Exec: func(_ context.Context, _ string, args []string, onLine func(string)) error {
		log = append(log, strings.Join(args, " "))
		switch {
		case args[0] == "image" && args[1] == "inspect":
			if !pulled {
				pulled = false
				return errors.New("No such image")
			}
			onLine(user)
		case args[0] == "pull":
			pulled = true
		case args[0] == "run":
			onLine(ids[args[len(args)-2]+args[len(args)-1]]) // e.g. "-ufilebrowser"
		}
		return nil
	}}, &log
}

func TestImageUser(t *testing.T) { // A09.11
	ctx := context.Background()
	cases := []struct {
		name, user string
		pulled     bool
		ids        map[string]string
		uid, gid   int
		ok         bool
	}{
		{"numeric", "1000", true, nil, 1000, -1, true},
		{"numeric with group", "1000:100", true, nil, 1000, 100, true},
		{"root", "", true, nil, 0, 0, false},
		{"explicit root", "0:0", true, nil, 0, 0, false},
		{"name resolved inside the image", "filebrowser", true, map[string]string{"-ufilebrowser": "1000", "-gfilebrowser": "1001"}, 1000, 1001, true},
		{"name with numeric group", "app:20", true, map[string]string{"-uapp": "999"}, 999, 20, true},
		{"not pulled yet", "1000", false, nil, 1000, -1, true},
		{"unresolvable name", "ghost", true, map[string]string{}, 0, 0, false},
		{"hostile name", "$(reboot)", true, nil, 0, 0, false},
	}
	for _, c := range cases {
		r, _ := imageExec(c.user, c.pulled, c.ids)
		uid, gid, ok := r.ImageUser(ctx, "img:1")
		if uid != c.uid || gid != c.gid || ok != c.ok {
			t.Errorf("%s: got %d %d %v, want %d %d %v", c.name, uid, gid, ok, c.uid, c.gid, c.ok)
		}
	}
	if _, _, ok := (&ExecRunner{Exec: func(context.Context, string, []string, func(string)) error { return errors.New("boom") }}).ImageUser(ctx, "img"); ok {
		t.Error("docker failures must mean unknown")
	}
	r, log := imageExec("1000", false, nil)
	r.ImageUser(ctx, "img:1")
	if len(*log) != 3 || !strings.HasPrefix((*log)[1], "pull -q") {
		t.Errorf("an image that is not local is pulled once, then inspected again: %q", *log)
	}
}
