// Package system collects host metrics (SPEC-02). Parsing is split from I/O so it can be
// tested against /proc fixtures on any platform.
package system

import (
	"bufio"
	"fmt"
	"strconv"
	"strings"
)

// cpuTimes returns (total, idle) jiffies from the aggregate "cpu " line of /proc/stat.
func parseCPUTimes(stat string) (total, idle uint64, err error) {
	sc := bufio.NewScanner(strings.NewReader(stat))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 5 || f[0] != "cpu" {
			continue
		}
		var vals []uint64
		for _, s := range f[1:] {
			v, perr := strconv.ParseUint(s, 10, 64)
			if perr != nil {
				return 0, 0, perr
			}
			vals = append(vals, v)
		}
		// user nice system idle iowait irq softirq steal guest guest_nice
		// guest/guest_nice are already counted in user/nice, so only sum the first 8 fields.
		n := len(vals)
		if n > 8 {
			n = 8
		}
		for _, v := range vals[:n] {
			total += v
		}
		idle = vals[3]
		if len(vals) > 4 {
			idle += vals[4] // iowait counts as idle
		}
		return total, idle, nil
	}
	return 0, 0, fmt.Errorf("cpu line not found in /proc/stat")
}

// cpuPercent computes utilisation between two samples (R02.4).
func cpuPercent(prevTotal, prevIdle, total, idle uint64) float64 {
	if total <= prevTotal {
		return 0
	}
	dTotal := float64(total - prevTotal)
	dIdle := float64(idle - prevIdle)
	p := (dTotal - dIdle) / dTotal * 100
	if p < 0 {
		return 0
	}
	if p > 100 {
		return 100
	}
	return p
}

type memInfo struct {
	Total, Available, SwapTotal, SwapFree uint64
}

func parseMeminfo(s string) (memInfo, error) {
	var m memInfo
	var haveTotal, haveAvail bool
	sc := bufio.NewScanner(strings.NewReader(s))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 2 {
			continue
		}
		v, err := strconv.ParseUint(f[1], 10, 64)
		if err != nil {
			continue
		}
		v *= 1024 // values are in kB
		switch f[0] {
		case "MemTotal:":
			m.Total, haveTotal = v, true
		case "MemAvailable:":
			m.Available, haveAvail = v, true
		case "SwapTotal:":
			m.SwapTotal = v
		case "SwapFree:":
			m.SwapFree = v
		}
	}
	if !haveTotal || !haveAvail {
		return m, fmt.Errorf("MemTotal/MemAvailable missing in /proc/meminfo")
	}
	return m, nil
}

func parseLoadavg(s string) ([3]float64, error) {
	f := strings.Fields(s)
	var out [3]float64
	if len(f) < 3 {
		return out, fmt.Errorf("unexpected /proc/loadavg")
	}
	for i := 0; i < 3; i++ {
		v, err := strconv.ParseFloat(f[i], 64)
		if err != nil {
			return out, err
		}
		out[i] = v
	}
	return out, nil
}

func parseUptime(s string) (uint64, error) {
	f := strings.Fields(s)
	if len(f) < 1 {
		return 0, fmt.Errorf("unexpected /proc/uptime")
	}
	v, err := strconv.ParseFloat(f[0], 64)
	if err != nil {
		return 0, err
	}
	return uint64(v), nil
}

func parseNetDev(s string) []NetIface {
	var out []NetIface
	sc := bufio.NewScanner(strings.NewReader(s))
	for sc.Scan() {
		name, rest, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue // header lines
		}
		name = strings.TrimSpace(name)
		f := strings.Fields(rest)
		if len(f) < 9 || name == "lo" {
			continue
		}
		rx, err1 := strconv.ParseUint(f[0], 10, 64)
		tx, err2 := strconv.ParseUint(f[8], 10, 64)
		if err1 != nil || err2 != nil {
			continue
		}
		out = append(out, NetIface{Name: name, RxBytes: rx, TxBytes: tx})
	}
	return out
}
