package docker

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
)

// frame builds one multiplexed log frame.
func frame(stream byte, payload string) []byte {
	h := make([]byte, 8)
	h[0] = stream
	binary.BigEndian.PutUint32(h[4:], uint32(len(payload)))
	return append(h, payload...)
}

func TestDemuxLogsReassemblesLines(t *testing.T) { // A03.2
	var buf bytes.Buffer
	buf.Write(frame(1, "hello\nwor"))
	buf.Write(frame(2, "err1\n"))
	buf.Write(frame(1, "ld\nlast-without-newline"))

	var got []LogLine
	if err := DemuxLogs(&buf, func(l LogLine) error { got = append(got, l); return nil }); err != nil {
		t.Fatal(err)
	}
	want := []LogLine{
		{"stdout", "hello"}, {"stderr", "err1"}, {"stdout", "world"}, {"stdout", "last-without-newline"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

func TestParseRunCommand(t *testing.T) { // A03.3
	spec, err := ParseRunCommand(`docker run -d --name x -p 8080:80 -v /a:/b -e A=1 --restart always img`)
	if err != nil {
		t.Fatal(err)
	}
	want := RunSpec{
		Image: "img", Name: "x", Restart: "always",
		Ports:   []RunPort{{Host: 8080, Container: 80}},
		Volumes: []RunVolume{{Host: "/a", Container: "/b"}},
		Env:     map[string]string{"A": "1"},
		Command: []string{},
	}
	if !reflect.DeepEqual(spec, want) {
		t.Fatalf("got %+v\nwant %+v", spec, want)
	}
}

func TestParseRunCommandAdvanced(t *testing.T) {
	cmd := "docker run -d \\\n  --name=web --net host -u 1000:1000 -h box --privileged \\\n  -p 127.0.0.1:53:53/udp -v \"/my data:/data:ro\" -e 'MSG=hello world' nginx:alpine nginx -g 'daemon off;'"
	s, err := ParseRunCommand(cmd)
	if err != nil {
		t.Fatal(err)
	}
	if s.Name != "web" || s.Network != "host" || s.User != "1000:1000" || s.Hostname != "box" || !s.Privileged {
		t.Errorf("flags: %+v", s)
	}
	if !reflect.DeepEqual(s.Ports, []RunPort{{HostIP: "127.0.0.1", Host: 53, Container: 53, Protocol: "udp"}}) {
		t.Errorf("ports: %+v", s.Ports)
	}
	if !reflect.DeepEqual(s.Volumes, []RunVolume{{Host: "/my data", Container: "/data", ReadOnly: true}}) {
		t.Errorf("volumes: %+v", s.Volumes)
	}
	if s.Env["MSG"] != "hello world" || s.Image != "nginx:alpine" {
		t.Errorf("env/image: %+v", s)
	}
	if !reflect.DeepEqual(s.Command, []string{"nginx", "-g", "daemon off;"}) {
		t.Errorf("command: %+v", s.Command)
	}
}

func TestParseRunCommandErrors(t *testing.T) {
	cases := map[string]string{
		"docker run --rm --memory=1g img": "--rm, --memory",
		"docker ps":                       "docker run",
		"docker run -d":                   "imagem",
		"docker run -p abc img":           "porta inválida",
		"docker run -e":                   "exige um valor",
		`docker run -e "A=1 img`:          "aspas",
	}
	for in, wantSub := range cases {
		_, err := ParseRunCommand(in)
		var ve *ValidationError
		if err == nil || !strings.Contains(err.Error(), wantSub) {
			t.Errorf("%q: err = %v, want containing %q", in, err, wantSub)
			continue
		}
		if !errors.As(err, &ve) {
			t.Errorf("%q: not a ValidationError: %T", in, err)
		}
	}
}

func TestEnvMasking(t *testing.T) { // A03.6
	env := ParseEnv([]string{"DB_PASSWORD=x", "API_TOKEN=y", "SECRET_THING=z", "SSH_KEY=k", "TZ=UTC"})
	for _, e := range env[:4] {
		if !e.Masked || e.Value != maskedValue {
			t.Errorf("%s should be masked: %+v", e.Key, e)
		}
	}
	if env[4].Masked || env[4].Value != "UTC" {
		t.Errorf("TZ must not be masked: %+v", env[4])
	}
}

func TestValidRef(t *testing.T) { // R03.11
	for _, ok := range []string{"abc123", "my-app_1.0", "a"} {
		if !ValidRef(ok) {
			t.Errorf("%q should be valid", ok)
		}
	}
	for _, bad := range []string{"", "../etc", "a/b", "-x", "a b", "a?b", strings.Repeat("a", 129)} {
		if ValidRef(bad) {
			t.Errorf("%q should be invalid", bad)
		}
	}
}

func TestSplitImageRef(t *testing.T) {
	cases := map[string][2]string{
		"nginx":                      {"nginx", "latest"},
		"nginx:1.25":                 {"nginx", "1.25"},
		"localhost:5000/app":         {"localhost:5000/app", "latest"},
		"localhost:5000/app:v2":      {"localhost:5000/app", "v2"},
		"repo/img@sha256:abcdef0123": {"repo/img@sha256:abcdef0123", ""},
	}
	for in, want := range cases {
		img, tag := SplitImageRef(in)
		if img != want[0] || tag != want[1] {
			t.Errorf("%s: got (%s,%s) want %v", in, img, tag, want)
		}
	}
}

// fakeDaemon records calls and serves canned answers.
type fakeDaemon struct {
	mu    sync.Mutex
	calls []string
	srv   *httptest.Server
	// imageMissing makes the first create answer "No such image".
	imageMissing bool
	createBody   map[string]any
}

func newFakeDaemon(t *testing.T) (*fakeDaemon, *RealClient) {
	t.Helper()
	f := &fakeDaemon{}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f, NewHTTPClient(f.srv.URL)
}

func (f *fakeDaemon) record(r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, r.Method+" "+strings.TrimPrefix(r.URL.RequestURI(), apiPrefix))
}

func (f *fakeDaemon) serve(w http.ResponseWriter, r *http.Request) {
	f.record(r)
	path := strings.TrimPrefix(r.URL.Path, apiPrefix)
	w.Header().Set("Content-Type", "application/json")
	switch {
	case path == "/containers/json":
		_, _ = w.Write([]byte(`[{"Id":"abcdef1234567890","Names":["/web"],"Image":"nginx","ImageID":"sha256:img1aaaaaaaaaaaa","State":"running","Status":"Up 1 hour","Created":1700000000,
		"Labels":{"omvhome.app":"web"},"Ports":[{"PrivatePort":80,"PublicPort":8080,"Type":"tcp","IP":"0.0.0.0"},{"PrivatePort":80,"PublicPort":8080,"Type":"tcp","IP":"::"},{"PrivatePort":443,"Type":"tcp"}]}]`))
	case path == "/containers/missing/json":
		w.WriteHeader(404)
		_, _ = w.Write([]byte(`{"message":"No such container: missing"}`))
	case path == "/containers/abc/json":
		_, _ = w.Write([]byte(`{"Id":"abc1234567890","Name":"/web","Created":"2026-01-02T03:04:05Z","Config":{"Image":"nginx","Env":["DB_PASSWORD=hunter2","TZ=UTC"],"Labels":{},"Tty":false,"Cmd":["nginx"]},
		"State":{"Status":"running"},"HostConfig":{"RestartPolicy":{"Name":"always"}},"Mounts":[{"Type":"bind","Source":"/a","Destination":"/b","RW":false}],
		"NetworkSettings":{"Networks":{"bridge":{}},"Ports":{"80/tcp":[{"HostPort":"8080"},{"HostPort":"8080"}]}}}`))
	case path == "/containers/abc/start" && r.Method == "POST":
		w.WriteHeader(304) // already started: must be treated as success
	case path == "/containers/abc/stop", path == "/containers/abc/restart":
		w.WriteHeader(204)
	case path == "/containers/abc" && r.Method == "DELETE":
		w.WriteHeader(204)
	case path == "/containers/abc/logs":
		w.Header().Set("Content-Type", "application/vnd.docker.raw-stream")
		_, _ = w.Write(append(frame(1, "line1\n"), frame(2, "oops\n")...))
	case path == "/containers/create":
		f.mu.Lock()
		missing := f.imageMissing
		f.imageMissing = false
		f.mu.Unlock()
		if missing {
			w.WriteHeader(404)
			_, _ = w.Write([]byte(`{"message":"No such image: nginx:latest"}`))
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&f.createBody)
		w.WriteHeader(201)
		_, _ = w.Write([]byte(`{"Id":"deadbeefcafe1234567890"}`))
	case path == "/containers/deadbeefcafe1234567890/start":
		w.WriteHeader(204)
	case path == "/images/create":
		_, _ = w.Write([]byte(`{"status":"Pulling from library/nginx","id":"latest"}` + "\n" + `{"status":"Downloading","id":"l1","progress":"[=>  ]"}` + "\n"))
	case path == "/containers/abc/stats":
		_, _ = w.Write([]byte(`{"cpu_stats":{"cpu_usage":{"total_usage":300},"system_cpu_usage":2000,"online_cpus":2},"precpu_stats":{"cpu_usage":{"total_usage":100},"system_cpu_usage":1000},
		"memory_stats":{"usage":1000,"limit":5000,"stats":{"inactive_file":200}},"networks":{"eth0":{"rx_bytes":10,"tx_bytes":20},"eth1":{"rx_bytes":1,"tx_bytes":2}}}`))
	case path == "/images/json":
		_, _ = w.Write([]byte(`[{"Id":"sha256:img1aaaaaaaaaaaa","RepoTags":["nginx:latest"],"Size":100,"Created":1700000000},{"Id":"sha256:img2bbbbbbbbbbbb","RepoTags":null,"Size":5,"Created":1700000000}]`))
	case path == "/version":
		_, _ = w.Write([]byte(`{"Version":"27.0.1","ApiVersion":"1.46"}`))
	default:
		w.WriteHeader(404)
		_, _ = w.Write([]byte(`{"message":"unhandled ` + path + `"}`))
	}
}

func TestRealClientListAndActions(t *testing.T) { // A03.1
	f, c := newFakeDaemon(t)
	ctx := context.Background()

	cs, err := c.ListContainers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 1 || cs[0].ID != "abcdef123456" || cs[0].Name != "web" || cs[0].AppID != "web" {
		t.Fatalf("containers: %+v", cs)
	}
	if !reflect.DeepEqual(cs[0].Ports, []Port{{Private: 80, Public: 8080, Protocol: "tcp"}}) {
		t.Errorf("ports (dedupe v4/v6, skip unpublished): %+v", cs[0].Ports)
	}

	for _, step := range []struct {
		name string
		fn   func() error
	}{
		{"start", func() error { return c.StartContainer(ctx, "abc") }},
		{"stop", func() error { return c.StopContainer(ctx, "abc") }},
		{"restart", func() error { return c.RestartContainer(ctx, "abc") }},
		{"remove", func() error { return c.RemoveContainer(ctx, "abc", true, true) }},
	} {
		if err := step.fn(); err != nil {
			t.Errorf("%s: %v", step.name, err)
		}
	}
	want := []string{
		"GET /containers/json?all=1",
		"POST /containers/abc/start",
		"POST /containers/abc/stop?t=10",
		"POST /containers/abc/restart?t=10",
		"DELETE /containers/abc?force=true&v=true",
	}
	if !reflect.DeepEqual(f.calls, want) {
		t.Fatalf("calls:\n got %v\nwant %v", f.calls, want)
	}

	if _, err := c.InspectContainer(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestRealClientInspectMasksEnv(t *testing.T) {
	_, c := newFakeDaemon(t)
	d, err := c.InspectContainer(context.Background(), "abc")
	if err != nil {
		t.Fatal(err)
	}
	if d.Env[0].Value != maskedValue || d.Env[1].Value != "UTC" {
		t.Errorf("env: %+v", d.Env)
	}
	if d.RestartPolicy != "always" || len(d.Mounts) != 1 || !d.Mounts[0].ReadOnly || len(d.Ports) != 1 {
		t.Errorf("detail: %+v", d)
	}
}

func TestRealClientLogs(t *testing.T) {
	_, c := newFakeDaemon(t)
	var got []LogLine
	if err := c.StreamLogs(context.Background(), "abc", 50, false, func(l LogLine) error { got = append(got, l); return nil }); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []LogLine{{"stdout", "line1"}, {"stderr", "oops"}}) {
		t.Fatalf("%+v", got)
	}
}

func TestRealClientRunPullsMissingImage(t *testing.T) {
	f, c := newFakeDaemon(t)
	f.imageMissing = true
	var events []PullEvent
	id, err := c.Run(context.Background(), RunSpec{
		Image: "nginx", Name: "web", Restart: "unless-stopped",
		Ports: []RunPort{{Host: 8081, Container: 80}}, Volumes: []RunVolume{{Host: "/srv/x", Container: "/x", ReadOnly: true}},
		Env: map[string]string{"B": "2", "A": "1"},
	}, func(e PullEvent) { events = append(events, e) })
	if err != nil {
		t.Fatal(err)
	}
	if id != "deadbeefcafe" || len(events) != 2 {
		t.Fatalf("id=%s events=%v", id, events)
	}
	want := []string{
		"POST /containers/create?name=web", "POST /images/create?fromImage=nginx&tag=latest",
		"POST /containers/create?name=web", "POST /containers/deadbeefcafe1234567890/start",
	}
	if !reflect.DeepEqual(f.calls, want) {
		t.Fatalf("calls:\n got %v\nwant %v", f.calls, want)
	}
	host := f.createBody["HostConfig"].(map[string]any)
	if !reflect.DeepEqual(host["Binds"], []any{"/srv/x:/x:ro"}) {
		t.Errorf("binds: %v", host["Binds"])
	}
	if !reflect.DeepEqual(f.createBody["Env"], []any{"A=1", "B=2"}) {
		t.Errorf("env should be sorted: %v", f.createBody["Env"])
	}
	pb := host["PortBindings"].(map[string]any)["80/tcp"].([]any)[0].(map[string]any)
	if pb["HostPort"] != "8081" {
		t.Errorf("port binding: %v", pb)
	}
}

func TestRunRejectsInvalidSpecBeforeCallingDocker(t *testing.T) {
	f, c := newFakeDaemon(t)
	bad := []RunSpec{
		{Image: ""},
		{Image: "x", Name: "../evil"},
		{Image: "x", Restart: "sometimes"},
		{Image: "x", Ports: []RunPort{{Host: 1, Container: 70000}}},
		{Image: "x", Volumes: []RunVolume{{Host: "/a", Container: "relative"}}},
		{Image: "x", Env: map[string]string{"1BAD": "v"}},
	}
	for _, s := range bad {
		_, err := c.Run(context.Background(), s, nil)
		var ve *ValidationError
		if !errors.As(err, &ve) {
			t.Errorf("%+v: want ValidationError, got %v", s, err)
		}
	}
	if len(f.calls) != 0 {
		t.Fatalf("docker must not be called: %v", f.calls)
	}
}

func TestRealClientStatsAndImages(t *testing.T) {
	_, c := newFakeDaemon(t)
	st, err := c.ContainerStats(context.Background(), "abc")
	if err != nil {
		t.Fatal(err)
	}
	// cpuDelta=200 sysDelta=1000 cpus=2 => 40%
	if st.CPUPercent != 40 || st.MemoryUsed != 800 || st.NetRx != 11 || st.NetTx != 22 {
		t.Errorf("%+v", st)
	}
	imgs, err := c.ListImages(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(imgs) != 2 || !imgs[0].InUse || imgs[1].InUse || imgs[1].Tags == nil {
		t.Errorf("%+v", imgs)
	}
}

func TestUnavailableDaemon(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close() // nothing listens any more
	_, err := NewHTTPClient(url).Ping(context.Background())
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("want ErrUnavailable, got %v", err)
	}
}

func TestMockSatisfiesClientAndRoundTrips(t *testing.T) {
	var c Client = NewMock()
	ctx := context.Background()
	cs, _ := c.ListContainers(ctx)
	if len(cs) < 4 {
		t.Fatalf("seed too small: %d", len(cs))
	}
	if err := c.StopContainer(ctx, "jellyfin"); err != nil {
		t.Fatal(err)
	}
	d, _ := c.InspectContainer(ctx, "jellyfin")
	if d.State != "exited" {
		t.Errorf("state %s", d.State)
	}
	id, err := c.Run(ctx, RunSpec{Image: "redis:7", Name: "cache"}, nil)
	if err != nil || id == "" {
		t.Fatal(id, err)
	}
	if err := c.RemoveContainer(ctx, "cache", false, false); err == nil {
		t.Error("removing a running container without force should fail")
	}
	if err := c.RemoveContainer(ctx, "cache", true, false); err != nil {
		t.Error(err)
	}
}
