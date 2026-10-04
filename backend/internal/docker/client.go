package docker

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const apiPrefix = "/v1.41"

// RealClient implements Client against a Docker daemon.
type RealClient struct {
	base string
	hc   *http.Client
}

// NewUnixClient connects through a unix socket such as /var/run/docker.sock.
func NewUnixClient(socket string) *RealClient {
	return &RealClient{
		base: "http://docker",
		hc: &http.Client{Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", socket)
			},
		}},
	}
}

// NewHTTPClient targets an arbitrary base URL (used by tests with httptest servers).
func NewHTTPClient(baseURL string) *RealClient {
	return &RealClient{base: strings.TrimRight(baseURL, "/"), hc: &http.Client{}}
}

// apiError is a non-2xx answer from the daemon.
type apiError struct {
	Status  int
	Message string
}

func (e *apiError) Error() string { return fmt.Sprintf("docker: %s (HTTP %d)", e.Message, e.Status) }

func (e *apiError) Is(target error) bool {
	return target == ErrNotFound && e.Status == http.StatusNotFound
}

// IsNoSuchImage reports a 404 caused by a missing image on container create.
func isNoSuchImage(err error) bool {
	var ae *apiError
	return errors.As(err, &ae) && ae.Status == http.StatusNotFound && strings.Contains(strings.ToLower(ae.Message), "no such image")
}

// request performs a call. Non-streaming calls get a 60s timeout; the caller must close the body.
func (c *RealClient) request(parent context.Context, method, path string, q url.Values, body any, stream bool) (*http.Response, context.CancelFunc, error) {
	ctx := parent
	cancel := context.CancelFunc(func() {})
	if !stream {
		ctx, cancel = context.WithTimeout(ctx, 60*time.Second)
	}
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			cancel()
			return nil, nil, err
		}
		rdr = bytes.NewReader(b)
	}
	u := c.base + apiPrefix + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rdr)
	if err != nil {
		cancel()
		return nil, nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		cancel()
		if parent.Err() != nil {
			return nil, nil, parent.Err()
		}
		return nil, nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	if resp.StatusCode >= 400 {
		defer resp.Body.Close()
		defer cancel()
		var m struct {
			Message string `json:"message"`
		}
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
		if json.Unmarshal(raw, &m) != nil || m.Message == "" {
			m.Message = strings.TrimSpace(string(raw))
		}
		return nil, nil, &apiError{Status: resp.StatusCode, Message: m.Message}
	}
	return resp, cancel, nil
}

// getJSON decodes a JSON answer into out.
func (c *RealClient) getJSON(ctx context.Context, path string, q url.Values, out any) error {
	resp, cancel, err := c.request(ctx, http.MethodGet, path, q, nil, false)
	if err != nil {
		return err
	}
	defer cancel()
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(out)
}

// act runs a POST/DELETE whose answer body is ignored.
func (c *RealClient) act(ctx context.Context, method, path string, q url.Values) error {
	resp, cancel, err := c.request(ctx, method, path, q, nil, false)
	if err != nil {
		return err
	}
	defer cancel()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.Body.Close()
}

func (c *RealClient) Ping(ctx context.Context) (Version, error) {
	var v struct{ Version, ApiVersion string }
	if err := c.getJSON(ctx, "/version", nil, &v); err != nil {
		return Version{}, err
	}
	return Version{Version: v.Version, APIVersion: v.ApiVersion}, nil
}

type rawContainer struct {
	ID      string `json:"Id"`
	Names   []string
	Image   string
	ImageID string
	State   string
	Status  string
	Created int64
	Labels  map[string]string
	Ports   []struct {
		PrivatePort int
		PublicPort  int
		Type        string
	}
}

func toContainer(r rawContainer) Container {
	name := ""
	if len(r.Names) > 0 {
		name = strings.TrimPrefix(r.Names[0], "/")
	}
	c := Container{
		ID: shortID(r.ID), Name: name, Image: r.Image, State: r.State, Status: r.Status,
		CreatedAt: time.Unix(r.Created, 0).UTC(), AppID: r.Labels[LabelApp], Labels: r.Labels,
		Ports: []Port{},
	}
	seen := map[Port]bool{}
	for _, p := range r.Ports {
		if p.PublicPort == 0 {
			continue
		}
		pp := Port{Private: p.PrivatePort, Public: p.PublicPort, Protocol: p.Type}
		if !seen[pp] { // Docker lists IPv4 and IPv6 bindings separately
			seen[pp] = true
			c.Ports = append(c.Ports, pp)
		}
	}
	return c
}

func shortID(id string) string {
	id = strings.TrimPrefix(id, "sha256:")
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

func (c *RealClient) ListContainers(ctx context.Context) ([]Container, error) {
	var raw []rawContainer
	if err := c.getJSON(ctx, "/containers/json", url.Values{"all": {"1"}}, &raw); err != nil {
		return nil, err
	}
	out := make([]Container, 0, len(raw))
	for _, r := range raw {
		out = append(out, toContainer(r))
	}
	return out, nil
}

func (c *RealClient) InspectContainer(ctx context.Context, id string) (*Detail, error) {
	var r struct {
		ID      string `json:"Id"`
		Name    string
		Created time.Time
		Config  struct {
			Image  string
			Env    []string
			Labels map[string]string
			Tty    bool
			Cmd    []string
		}
		State struct {
			Status string
		}
		HostConfig struct {
			RestartPolicy struct{ Name string }
		}
		Mounts []struct {
			Type, Source, Destination string
			RW                        bool
		}
		NetworkSettings struct {
			Networks map[string]json.RawMessage
			Ports    map[string][]struct{ HostPort string }
		}
	}
	if err := c.getJSON(ctx, "/containers/"+url.PathEscape(id)+"/json", nil, &r); err != nil {
		return nil, err
	}
	d := &Detail{
		Container: Container{
			ID: shortID(r.ID), Name: strings.TrimPrefix(r.Name, "/"), Image: r.Config.Image,
			State: r.State.Status, Status: r.State.Status, CreatedAt: r.Created.UTC(),
			AppID: r.Config.Labels[LabelApp], Labels: r.Config.Labels, Ports: []Port{},
		},
		Env:           ParseEnv(r.Config.Env),
		Mounts:        []Mount{},
		Networks:      []string{},
		RestartPolicy: r.HostConfig.RestartPolicy.Name,
		Tty:           r.Config.Tty,
		Command:       r.Config.Cmd,
	}
	for _, m := range r.Mounts {
		d.Mounts = append(d.Mounts, Mount{Type: m.Type, Source: m.Source, Destination: m.Destination, ReadOnly: !m.RW})
	}
	for n := range r.NetworkSettings.Networks {
		d.Networks = append(d.Networks, n)
	}
	sort.Strings(d.Networks)
	for spec, binds := range r.NetworkSettings.Ports {
		priv, proto, _ := strings.Cut(spec, "/")
		pn, _ := strconv.Atoi(priv)
		seen := map[int]bool{}
		for _, b := range binds {
			hp, _ := strconv.Atoi(b.HostPort)
			if hp > 0 && !seen[hp] {
				seen[hp] = true
				d.Ports = append(d.Ports, Port{Private: pn, Public: hp, Protocol: proto})
			}
		}
	}
	sort.Slice(d.Ports, func(i, j int) bool { return d.Ports[i].Private < d.Ports[j].Private })
	return d, nil
}

func (c *RealClient) StartContainer(ctx context.Context, id string) error {
	return c.act(ctx, http.MethodPost, "/containers/"+url.PathEscape(id)+"/start", nil) // 304 = already started
}

func (c *RealClient) StopContainer(ctx context.Context, id string) error {
	return c.act(ctx, http.MethodPost, "/containers/"+url.PathEscape(id)+"/stop", url.Values{"t": {"10"}})
}

func (c *RealClient) RestartContainer(ctx context.Context, id string) error {
	return c.act(ctx, http.MethodPost, "/containers/"+url.PathEscape(id)+"/restart", url.Values{"t": {"10"}})
}

func (c *RealClient) RemoveContainer(ctx context.Context, id string, force, volumes bool) error {
	q := url.Values{"force": {strconv.FormatBool(force)}, "v": {strconv.FormatBool(volumes)}}
	return c.act(ctx, http.MethodDelete, "/containers/"+url.PathEscape(id), q)
}

func (c *RealClient) StreamLogs(ctx context.Context, id string, tail int, follow bool, fn func(LogLine) error) error {
	d, err := c.InspectContainer(ctx, id)
	if err != nil {
		return err
	}
	q := url.Values{"stdout": {"1"}, "stderr": {"1"}, "follow": {strconv.FormatBool(follow)}}
	if tail > 0 {
		q.Set("tail", strconv.Itoa(tail))
	} else {
		q.Set("tail", "all")
	}
	resp, cancel, err := c.request(ctx, http.MethodGet, "/containers/"+url.PathEscape(id)+"/logs", q, nil, true)
	if err != nil {
		return err
	}
	defer cancel()
	defer resp.Body.Close()
	var rerr error
	if d.Tty {
		rerr = rawLines(resp.Body, fn)
	} else {
		rerr = DemuxLogs(resp.Body, fn)
	}
	if ctx.Err() != nil {
		return nil // cancelled by the caller: normal end of a follow
	}
	return rerr
}

// rawLines splits a TTY log stream (no multiplexing) into lines.
func rawLines(r io.Reader, fn func(LogLine) error) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		if err := fn(LogLine{Stream: "stdout", Text: strings.TrimRight(sc.Text(), "\r")}); err != nil {
			return err
		}
	}
	return sc.Err()
}

// DemuxLogs decodes Docker's multiplexed stream: 8-byte header (type, 0,0,0, size big-endian)
// followed by the payload. Frames may split lines, so lines are reassembled per stream (R03.4).
func DemuxLogs(r io.Reader, fn func(LogLine) error) error {
	br := bufio.NewReader(r)
	pending := map[string][]byte{}
	emit := func(stream string, data []byte, flush bool) error {
		buf := append(pending[stream], data...)
		for {
			i := bytes.IndexByte(buf, '\n')
			if i < 0 {
				break
			}
			line := strings.TrimRight(string(buf[:i]), "\r")
			buf = buf[i+1:]
			if err := fn(LogLine{Stream: stream, Text: line}); err != nil {
				return err
			}
		}
		if flush && len(buf) > 0 {
			if err := fn(LogLine{Stream: stream, Text: string(buf)}); err != nil {
				return err
			}
			buf = nil
		}
		pending[stream] = buf
		return nil
	}
	hdr := make([]byte, 8)
	for {
		if _, err := io.ReadFull(br, hdr); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				for _, s := range []string{"stdout", "stderr"} {
					if err := emit(s, nil, true); err != nil {
						return err
					}
				}
				return nil
			}
			return err
		}
		size := binary.BigEndian.Uint32(hdr[4:8])
		payload := make([]byte, size)
		if _, err := io.ReadFull(br, payload); err != nil {
			return err
		}
		stream := "stdout"
		if hdr[0] == 2 {
			stream = "stderr"
		}
		if err := emit(stream, payload, false); err != nil {
			return err
		}
	}
}

func (c *RealClient) ContainerStats(ctx context.Context, id string) (Stats, error) {
	var r struct {
		CPUStats    cpuStats `json:"cpu_stats"`
		PreCPUStats cpuStats `json:"precpu_stats"`
		MemoryStats struct {
			Usage uint64
			Limit uint64
			Stats map[string]uint64
		} `json:"memory_stats"`
		Networks map[string]struct {
			RxBytes uint64 `json:"rx_bytes"`
			TxBytes uint64 `json:"tx_bytes"`
		}
	}
	if err := c.getJSON(ctx, "/containers/"+url.PathEscape(id)+"/stats", url.Values{"stream": {"false"}}, &r); err != nil {
		return Stats{}, err
	}
	st := Stats{CPUPercent: cpuPercent(r.CPUStats, r.PreCPUStats), MemoryLimit: r.MemoryStats.Limit}
	cache := r.MemoryStats.Stats["inactive_file"] // cgroup v2
	if cache == 0 {
		cache = r.MemoryStats.Stats["cache"] // cgroup v1
	}
	if r.MemoryStats.Usage > cache {
		st.MemoryUsed = r.MemoryStats.Usage - cache
	}
	for _, n := range r.Networks {
		st.NetRx += n.RxBytes
		st.NetTx += n.TxBytes
	}
	return st, nil
}

type cpuStats struct {
	CPUUsage struct {
		TotalUsage uint64 `json:"total_usage"`
	} `json:"cpu_usage"`
	SystemUsage uint64 `json:"system_cpu_usage"`
	OnlineCPUs  int    `json:"online_cpus"`
}

func cpuPercent(cur, prev cpuStats) float64 {
	cpuDelta := float64(cur.CPUUsage.TotalUsage) - float64(prev.CPUUsage.TotalUsage)
	sysDelta := float64(cur.SystemUsage) - float64(prev.SystemUsage)
	if cpuDelta <= 0 || sysDelta <= 0 {
		return 0
	}
	n := float64(cur.OnlineCPUs)
	if n == 0 {
		n = 1
	}
	return float64(int(cpuDelta/sysDelta*n*1000+0.5)) / 10
}

func (c *RealClient) Run(ctx context.Context, spec RunSpec, onPull func(PullEvent)) (string, error) {
	if err := spec.Validate(); err != nil {
		return "", err
	}
	body := spec.createBody()
	q := url.Values{}
	if spec.Name != "" {
		q.Set("name", spec.Name)
	}
	create := func() (string, error) {
		resp, cancel, err := c.request(ctx, http.MethodPost, "/containers/create", q, body, false)
		if err != nil {
			return "", err
		}
		defer cancel()
		defer resp.Body.Close()
		var out struct{ Id string }
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			return "", err
		}
		return out.Id, nil
	}
	id, err := create()
	if isNoSuchImage(err) {
		if err := c.PullImage(ctx, spec.Image, onPull); err != nil {
			return "", err
		}
		id, err = create()
	}
	if err != nil {
		return "", err
	}
	if err := c.StartContainer(ctx, id); err != nil {
		_ = c.RemoveContainer(context.WithoutCancel(ctx), id, true, true) // don't leave a dead container behind
		return "", err
	}
	return shortID(id), nil
}

func (c *RealClient) ListImages(ctx context.Context) ([]Image, error) {
	var raw []struct {
		ID       string `json:"Id"`
		RepoTags []string
		Size     int64
		Created  int64
	}
	if err := c.getJSON(ctx, "/images/json", nil, &raw); err != nil {
		return nil, err
	}
	var cs []rawContainer
	if err := c.getJSON(ctx, "/containers/json", url.Values{"all": {"1"}}, &cs); err != nil {
		return nil, err
	}
	used := map[string]bool{}
	for _, c := range cs {
		used[c.ImageID] = true
	}
	out := make([]Image, 0, len(raw))
	for _, r := range raw {
		tags := r.RepoTags
		if tags == nil {
			tags = []string{}
		}
		out = append(out, Image{ID: shortID(r.ID), Tags: tags, Size: r.Size, CreatedAt: time.Unix(r.Created, 0).UTC(), InUse: used[r.ID]})
	}
	return out, nil
}

// SplitImageRef separates "repo[:tag]" or "repo@digest" into the fromImage/tag pair Docker expects.
func SplitImageRef(ref string) (image, tag string) {
	if strings.Contains(ref, "@") {
		return ref, ""
	}
	slash := strings.LastIndex(ref, "/")
	if colon := strings.LastIndex(ref, ":"); colon > slash {
		return ref[:colon], ref[colon+1:]
	}
	return ref, "latest"
}

func (c *RealClient) PullImage(ctx context.Context, ref string, onEvent func(PullEvent)) error {
	img, tag := SplitImageRef(ref)
	q := url.Values{"fromImage": {img}}
	if tag != "" {
		q.Set("tag", tag)
	}
	resp, cancel, err := c.request(ctx, http.MethodPost, "/images/create", q, nil, true)
	if err != nil {
		return err
	}
	defer cancel()
	defer resp.Body.Close()
	dec := json.NewDecoder(resp.Body)
	for {
		var ev struct {
			Status      string
			ID          string `json:"id"`
			Progress    string
			Error       string
			ErrorDetail struct{ Message string }
		}
		if err := dec.Decode(&ev); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if ev.Error != "" || ev.ErrorDetail.Message != "" {
			msg := ev.ErrorDetail.Message
			if msg == "" {
				msg = ev.Error
			}
			return fmt.Errorf("docker: pull %s: %s", ref, msg)
		}
		if onEvent != nil {
			onEvent(PullEvent{Status: ev.Status, ID: ev.ID, Progress: ev.Progress})
		}
	}
}

func (c *RealClient) RemoveImage(ctx context.Context, id string) error {
	return c.act(ctx, http.MethodDelete, "/images/"+url.PathEscape(id), url.Values{"force": {"false"}})
}

// ErrorStatus returns the HTTP status the daemon answered with, if err came from one.
func ErrorStatus(err error) (int, bool) {
	var ae *apiError
	if errors.As(err, &ae) {
		return ae.Status, true
	}
	return 0, false
}
