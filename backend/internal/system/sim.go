package system

import (
	"math"
	"sync"
	"time"
)

// SimCollector produces plausible, deterministic metrics (function of elapsed time) for
// --mock and non-Linux hosts.
type SimCollector struct {
	start time.Time
	now   func() time.Time
	mu    sync.Mutex
}

func NewSimCollector() *SimCollector {
	return &SimCollector{start: time.Now(), now: time.Now}
}

func (s *SimCollector) Info() (Info, error) {
	return Info{
		Hostname:      "omv-demo",
		Kernel:        "6.1.0-simulated",
		Cores:         4,
		UptimeSeconds: uint64(s.now().Sub(s.start).Seconds()) + 86400*3,
	}, nil
}

func (s *SimCollector) Stats() (Stats, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	t := now.Sub(s.start).Seconds()
	const gib = 1 << 30
	temp := round1(42 + 3*math.Sin(t/20))
	return Stats{
		Time: now.UTC(),
		CPU: CPU{
			Percent: round1(18 + 12*math.Sin(t/7)),
			Cores:   4,
			Load:    [3]float64{round1(0.4 + 0.2*math.Sin(t/9)), 0.35, 0.3},
		},
		Memory:      Memory{Total: 8 * gib, Used: uint64((2.4 + 0.2*math.Sin(t/30)) * gib), Available: 8*gib - uint64((2.4+0.2*math.Sin(t/30))*gib)},
		Swap:        Swap{Total: 2 * gib, Used: 64 << 20},
		Temperature: &temp,
		Network: []NetIface{{
			Name:    "eth0",
			RxBytes: uint64(t*350_000) + 4*gib,
			TxBytes: uint64(t*120_000) + gib,
		}},
	}, nil
}
