package apps

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"omvhome/internal/compose"
	"omvhome/internal/docker"
)

func c(name, state string, labels map[string]string) docker.Container {
	return docker.Container{ID: name, Name: name, State: state, Labels: labels}
}

func appL(id, project string) map[string]string {
	l := map[string]string{docker.LabelApp: id, docker.LabelName: strings.ToUpper(id[:1]) + id[1:], docker.LabelPort: "8080"}
	if project != "" {
		l[docker.ComposeProject] = project
	}
	return l
}

func TestGroupBuildsApps(t *testing.T) { // A04.1
	cs := []docker.Container{
		c("jellyfin", "running", appL("jellyfin", "omvhome-jellyfin")),
		c("nginx-test", "running", nil), // no labels: not an app
		c("nextcloud", "running", appL("nextcloud", "omvhome-nextcloud")),
		c("omvhome-nextcloud-db-1", "running", map[string]string{docker.ComposeProject: "omvhome-nextcloud"}), // sibling
		c("standalone", "exited", appL("standalone", "")),
	}
	got := Group(cs)
	if len(got) != 3 {
		t.Fatalf("want 3 apps, got %d: %+v", len(got), got)
	}
	by := map[string]App{}
	for _, a := range got {
		by[a.ID] = a
	}
	if !by["jellyfin"].Managed || by["standalone"].Managed {
		t.Errorf("managed flags wrong: %+v %+v", by["jellyfin"], by["standalone"])
	}
	if !reflect.DeepEqual(by["nextcloud"].Containers, []string{"nextcloud", "omvhome-nextcloud-db-1"}) {
		t.Errorf("siblings must join the app: %+v", by["nextcloud"].Containers)
	}
	if by["jellyfin"].Port != 8080 || by["jellyfin"].Scheme != "http" || by["jellyfin"].Category != "Other" || by["jellyfin"].Name != "Jellyfin" {
		t.Errorf("defaults: %+v", by["jellyfin"])
	}
	if got[0].ID != "jellyfin" { // sorted by display name: Jellyfin, Nextcloud, Standalone
		t.Errorf("order: %v", []string{got[0].ID, got[1].ID, got[2].ID})
	}
}

func TestAggregateState(t *testing.T) { // A04.2
	cases := []struct {
		in   []string
		want string
	}{
		{[]string{"running", "running"}, "running"},
		{[]string{"exited", "exited"}, "stopped"},
		{[]string{"running", "exited"}, "partial"},
		{nil, "stopped"},
		{[]string{"paused"}, "stopped"},
	}
	for _, tc := range cases {
		if got := AggregateState(tc.in); got != tc.want {
			t.Errorf("%v: got %s want %s", tc.in, got, tc.want)
		}
	}
}

type fakeCompose struct{ calls []string }

func (f *fakeCompose) rec(verb, project string) error {
	f.calls = append(f.calls, verb+" "+project)
	return nil
}
func (f *fakeCompose) Available(context.Context) bool { return true }
func (f *fakeCompose) Up(_ context.Context, p, _ string, _ func(string)) error {
	return f.rec("up", p)
}
func (f *fakeCompose) Start(_ context.Context, p, _ string) error   { return f.rec("start", p) }
func (f *fakeCompose) Stop(_ context.Context, p, _ string) error    { return f.rec("stop", p) }
func (f *fakeCompose) Restart(_ context.Context, p, _ string) error { return f.rec("restart", p) }
func (f *fakeCompose) Pull(_ context.Context, p, _ string, _ func(string)) error {
	return f.rec("pull", p)
}
func (f *fakeCompose) Down(_ context.Context, p, _ string) error { return f.rec("down", p) }

var _ compose.Runner = (*fakeCompose)(nil)

func setup(t *testing.T) (*Service, *fakeCompose, *docker.Mock) {
	t.Helper()
	dm := docker.NewMock() // seeds jellyfin/pihole/syncthing as managed-labelled apps
	fc := &fakeCompose{}
	root := t.TempDir()
	return &Service{Docker: dm, Compose: fc, DataDir: filepath.Join(root, "data"), AppDataDir: filepath.Join(root, "appdata")}, fc, dm
}

func writeProject(t *testing.T, s *Service, id string) {
	t.Helper()
	dir := s.ProjectDir(id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, compose.ComposeFile), []byte("services: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestStopManagedAppUsesCompose(t *testing.T) { // A04.3
	s, fc, _ := setup(t)
	writeProject(t, s, "jellyfin")
	if err := s.Control(context.Background(), "jellyfin", "stop"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(fc.calls, []string{"stop omvhome-jellyfin"}) {
		t.Fatalf("calls: %v", fc.calls)
	}
}

func TestControlFallsBackToContainers(t *testing.T) {
	s, fc, dm := setup(t) // no project files on disk
	if err := s.Control(context.Background(), "jellyfin", "stop"); err != nil {
		t.Fatal(err)
	}
	if len(fc.calls) != 0 {
		t.Fatalf("compose must not be used: %v", fc.calls)
	}
	d, _ := dm.InspectContainer(context.Background(), "jellyfin")
	if d.State != "exited" {
		t.Fatalf("container state %s", d.State)
	}
	if err := s.Control(context.Background(), "ghost", "stop"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	if err := s.Control(context.Background(), "../x", "stop"); !errors.Is(err, ErrBadID) {
		t.Fatalf("want ErrBadID, got %v", err)
	}
}

func TestUpdateRequiresManaged(t *testing.T) {
	s, fc, _ := setup(t)
	if err := s.Update(context.Background(), "jellyfin", nil); !errors.Is(err, ErrNotManaged) {
		t.Fatalf("want ErrNotManaged, got %v", err)
	}
	writeProject(t, s, "jellyfin")
	if err := s.Update(context.Background(), "jellyfin", nil); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(fc.calls, []string{"pull omvhome-jellyfin", "up omvhome-jellyfin"}) {
		t.Fatalf("calls: %v", fc.calls)
	}
}

func TestUninstallManagedKeepsDataUnlessPurge(t *testing.T) {
	s, fc, _ := setup(t)
	writeProject(t, s, "jellyfin")
	data := filepath.Join(s.AppDataDir, "jellyfin", "config")
	if err := os.MkdirAll(data, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := s.Uninstall(context.Background(), "jellyfin", false); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(fc.calls, []string{"down omvhome-jellyfin"}) {
		t.Fatalf("calls: %v", fc.calls)
	}
	if _, err := os.Stat(s.ProjectDir("jellyfin")); !os.IsNotExist(err) {
		t.Error("project dir should be removed")
	}
	if _, err := os.Stat(data); err != nil {
		t.Error("data must survive without purge")
	}
}

func TestUninstallPurgeRemovesData(t *testing.T) {
	s, _, _ := setup(t)
	writeProject(t, s, "jellyfin")
	data := filepath.Join(s.AppDataDir, "jellyfin")
	if err := os.MkdirAll(filepath.Join(data, "config"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := s.Uninstall(context.Background(), "jellyfin", true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(data); !os.IsNotExist(err) {
		t.Error("data dir should be gone")
	}
}

func TestPurgeSafety(t *testing.T) { // A04.4
	s, fc, _ := setup(t)
	writeProject(t, s, "jellyfin")

	for _, root := range []string{"", "/", "."} {
		s.AppDataDir = root
		if err := s.Uninstall(context.Background(), "jellyfin", true); !errors.Is(err, ErrNoPurge) {
			t.Errorf("root %q: want ErrNoPurge, got %v", root, err)
		}
	}
	if len(fc.calls) != 0 {
		t.Fatalf("nothing may be destroyed when purge validation fails: %v", fc.calls)
	}
	if _, err := s.safeDataDir("../etc"); !errors.Is(err, ErrNoPurge) {
		t.Errorf("traversal id must be refused, got %v", err)
	}
}

func TestUninstallUnmanagedRemovesContainers(t *testing.T) {
	s, fc, dm := setup(t)
	dm.Add(docker.Detail{Container: docker.Container{Name: "solo", State: "running", Labels: appL("solo", "")}})
	if err := s.Uninstall(context.Background(), "solo", false); err != nil {
		t.Fatal(err)
	}
	if len(fc.calls) != 0 {
		t.Fatalf("compose used for unmanaged app: %v", fc.calls)
	}
	if _, err := dm.InspectContainer(context.Background(), "solo"); !errors.Is(err, docker.ErrNotFound) {
		t.Fatalf("container should be removed: %v", err)
	}
}

func (f *fakeCompose) SeedDir(context.Context, string, string, string) error { return nil }

func (f *fakeCompose) ImageUser(context.Context, string) (int, int, bool) { return 0, 0, false }
