package system

import (
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

type CPU struct {
	Percent float64    `json:"percent"`
	Cores   int        `json:"cores"`
	Load    [3]float64 `json:"load"`
}

type Memory struct {
	Total     uint64 `json:"total"`
	Used      uint64 `json:"used"`
	Available uint64 `json:"available"`
}

type Swap struct {
	Total uint64 `json:"total"`
	Used  uint64 `json:"used"`
}

type NetIface struct {
	Name    string `json:"name"`
	RxBytes uint64 `json:"rxBytes"`
	TxBytes uint64 `json:"txBytes"`
}

// Stats is a point-in-time snapshot (R02.2).
type Stats struct {
	Time        time.Time  `json:"time"`
	CPU         CPU        `json:"cpu"`
	Memory      Memory     `json:"memory"`
	Swap        Swap       `json:"swap"`
	Temperature *float64   `json:"temperature"`
	Network     []NetIface `json:"network"`
}

// Info is static host information (R02.1). OMVVersion is filled by the caller from OMV.
type Info struct {
	Hostname      string `json:"hostname"`
	OMVVersion    string `json:"omvVersion"`
	Kernel        string `json:"kernel"`
	UptimeSeconds uint64 `json:"uptimeSeconds"`
	Cores         int    `json:"cores"`
}

// Collector produces host metrics.
type Collector interface {
	Info() (Info, error)
	Stats() (Stats, error)
}

// New returns a /proc based collector on Linux and a deterministic simulation elsewhere (R02.8).
func New(forceSim bool) Collector {
	if !forceSim && runtime.GOOS == "linux" {
		return NewProcCollector("/")
	}
	return NewSimCollector()
}

// ProcCollector reads /proc and /sys below root ("/" in production, a fixture dir in tests).
type ProcCollector struct {
	root string

	mu                sync.Mutex
	prevTotal, prevID uint64
	havePrev          bool
	last              Stats
	lastAt            time.Time
	now               func() time.Time
	sleep             func(time.Duration)
}

func NewProcCollector(root string) *ProcCollector {
	return &ProcCollector{root: root, now: time.Now, sleep: time.Sleep}
}

func (c *ProcCollector) read(rel string) (string, error) {
	b, err := os.ReadFile(filepath.Join(c.root, rel))
	return string(b), err
}

func (c *ProcCollector) Info() (Info, error) {
	host, _ := os.Hostname()
	info := Info{Hostname: host, Cores: runtime.NumCPU()}
	if s, err := c.read("proc/sys/kernel/osrelease"); err == nil {
		info.Kernel = strings.TrimSpace(s)
	}
	if s, err := c.read("proc/uptime"); err == nil {
		if up, err := parseUptime(s); err == nil {
			info.UptimeSeconds = up
		}
	}
	return info, nil
}

func (c *ProcCollector) cpuSample() (total, idle uint64, err error) {
	s, err := c.read("proc/stat")
	if err != nil {
		return 0, 0, err
	}
	return parseCPUTimes(s)
}

func (c *ProcCollector) Stats() (Stats, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := c.now()
	if !c.lastAt.IsZero() && now.Sub(c.lastAt) < time.Second {
		return c.last, nil // avoid noisy deltas from back-to-back calls
	}

	total, idle, err := c.cpuSample()
	if err != nil {
		return Stats{}, err
	}
	if !c.havePrev {
		c.prevTotal, c.prevID, c.havePrev = total, idle, true
		c.sleep(200 * time.Millisecond)
		total, idle, err = c.cpuSample()
		if err != nil {
			return Stats{}, err
		}
		now = c.now()
	}
	st := Stats{Time: now.UTC(), CPU: CPU{Cores: runtime.NumCPU()}}
	st.CPU.Percent = round1(cpuPercent(c.prevTotal, c.prevID, total, idle))
	c.prevTotal, c.prevID = total, idle

	if s, err := c.read("proc/meminfo"); err == nil {
		if m, err := parseMeminfo(s); err == nil {
			st.Memory = Memory{Total: m.Total, Available: m.Available, Used: m.Total - m.Available}
			st.Swap = Swap{Total: m.SwapTotal, Used: m.SwapTotal - m.SwapFree}
		}
	}
	if s, err := c.read("proc/loadavg"); err == nil {
		st.CPU.Load, _ = parseLoadavg(s)
	}
	if s, err := c.read("proc/net/dev"); err == nil {
		st.Network = parseNetDev(s)
	}
	st.Temperature = c.temperature()

	c.last, c.lastAt = st, now
	return st, nil
}

// temperature returns the highest thermal zone reading in °C, or nil if none is available.
func (c *ProcCollector) temperature() *float64 {
	zones, _ := filepath.Glob(filepath.Join(c.root, "sys/class/thermal/thermal_zone*/temp"))
	var best *float64
	for _, z := range zones {
		b, err := os.ReadFile(z)
		if err != nil {
			continue
		}
		milli, err := strconv.ParseFloat(strings.TrimSpace(string(b)), 64)
		if err != nil {
			continue
		}
		v := round1(milli / 1000)
		if best == nil || v > *best {
			best = &v
		}
	}
	return best
}

func round1(v float64) float64 { return math.Round(v*10) / 10 }
