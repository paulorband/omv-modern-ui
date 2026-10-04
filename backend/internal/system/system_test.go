package system

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

const (
	stat1 = "cpu  1000 0 500 8000 500 0 0 0 0 0\ncpu0 500 0 250 4000 250 0 0 0 0 0\n"
	// +200 user, +100 system, +600 idle, +100 iowait => busy 300 of 1000 total delta = 30%
	stat2 = "cpu  1200 0 600 8600 600 0 0 0 0 0\ncpu0 600 0 300 4300 300 0 0 0 0 0\n"

	meminfo = "MemTotal:        8000000 kB\nMemFree:          500000 kB\nMemAvailable:    6000000 kB\nSwapTotal:       2000000 kB\nSwapFree:        1500000 kB\n"

	netdev = `Inter-|   Receive                                                |  Transmit
 face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed
    lo:  999  10    0    0    0     0          0         0   999  10    0    0    0     0       0          0
  eth0: 123456 100    0    0    0     0          0         0 654321  90    0    0    0     0       0          0
`
)

func TestParseCPUPercent(t *testing.T) { // A02.1
	t1, i1, err := parseCPUTimes(stat1)
	if err != nil {
		t.Fatal(err)
	}
	t2, i2, err := parseCPUTimes(stat2)
	if err != nil {
		t.Fatal(err)
	}
	if got := cpuPercent(t1, i1, t2, i2); got != 30 {
		t.Fatalf("cpu = %v, want 30", got)
	}
	if got := cpuPercent(t2, i2, t2, i2); got != 0 {
		t.Fatalf("no delta should be 0, got %v", got)
	}
}

func TestParseMeminfo(t *testing.T) { // A02.2
	m, err := parseMeminfo(meminfo)
	if err != nil {
		t.Fatal(err)
	}
	if m.Total != 8000000*1024 || m.Available != 6000000*1024 {
		t.Fatalf("%+v", m)
	}
	if _, err := parseMeminfo("MemTotal: 1 kB\n"); err == nil {
		t.Fatal("expected error when MemAvailable is missing")
	}
}

func TestParseNetDevSkipsLoopback(t *testing.T) {
	ifs := parseNetDev(netdev)
	if len(ifs) != 1 || ifs[0].Name != "eth0" || ifs[0].RxBytes != 123456 || ifs[0].TxBytes != 654321 {
		t.Fatalf("%+v", ifs)
	}
}

func TestParseLoadavgAndUptime(t *testing.T) {
	l, err := parseLoadavg("0.52 0.40 0.31 1/234 5678\n")
	if err != nil || l != [3]float64{0.52, 0.40, 0.31} {
		t.Fatalf("%v %v", l, err)
	}
	if up, err := parseUptime("3600.55 7000.10\n"); err != nil || up != 3600 {
		t.Fatalf("%d %v", up, err)
	}
}

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestProcCollectorStats(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "proc/stat", stat1)
	writeFile(t, root, "proc/meminfo", meminfo)
	writeFile(t, root, "proc/loadavg", "0.10 0.20 0.30 1/2 3\n")
	writeFile(t, root, "proc/net/dev", netdev)
	writeFile(t, root, "sys/class/thermal/thermal_zone0/temp", "41500\n")
	writeFile(t, root, "sys/class/thermal/thermal_zone1/temp", "38000\n")

	c := NewProcCollector(root)
	c.sleep = func(time.Duration) { writeFile(t, root, "proc/stat", stat2) } // 2nd sample arrives during the pause

	st, err := c.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if st.CPU.Percent != 30 {
		t.Errorf("cpu = %v", st.CPU.Percent)
	}
	if st.Memory.Used != 2000000*1024 || st.Swap.Used != 500000*1024 {
		t.Errorf("mem=%+v swap=%+v", st.Memory, st.Swap)
	}
	if st.Temperature == nil || *st.Temperature != 41.5 {
		t.Errorf("temp = %v", st.Temperature)
	}
	if len(st.Network) != 1 || st.CPU.Load[0] != 0.10 {
		t.Errorf("net=%+v load=%v", st.Network, st.CPU.Load)
	}
}

func TestProcCollectorMissingFilesDoNotFailOptionalMetrics(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "proc/stat", stat1)
	c := NewProcCollector(root)
	c.sleep = func(time.Duration) {}
	st, err := c.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if st.Temperature != nil || st.Network != nil {
		t.Fatalf("expected empty optional metrics: %+v", st)
	}
}

func TestSimCollectorIsDeterministicFunctionOfTime(t *testing.T) { // R02.8
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	mk := func() *SimCollector {
		return &SimCollector{start: base, now: func() time.Time { return base.Add(30 * time.Second) }}
	}
	a, _ := mk().Stats()
	b, _ := mk().Stats()
	if a.CPU.Percent != b.CPU.Percent || a.Memory.Used != b.Memory.Used {
		t.Fatalf("not deterministic: %+v vs %+v", a, b)
	}
	if a.Memory.Total == 0 || len(a.Network) == 0 {
		t.Fatalf("incomplete sim: %+v", a)
	}
}

func TestHubDeliversAndStops(t *testing.T) { // A02.3 (hub part)
	sim := NewSimCollector()
	h := NewHub(sim, 20*time.Millisecond)
	ch, unsub := h.Subscribe()
	for i := 0; i < 3; i++ {
		select {
		case <-ch:
		case <-time.After(time.Second):
			t.Fatalf("no sample %d", i)
		}
	}
	unsub()
	unsub() // idempotent
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.subs) != 0 || h.cancel != nil {
		t.Fatal("sampler should be stopped without subscribers")
	}
}
