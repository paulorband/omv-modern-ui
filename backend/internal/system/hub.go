package system

import (
	"context"
	"sync"
	"time"
)

// Hub samples a Collector on an interval and fans results out to subscribers. Sampling runs
// only while at least one subscriber is connected (R02.3).
type Hub struct {
	c        Collector
	interval time.Duration

	mu     sync.Mutex
	subs   map[chan Stats]struct{}
	cancel context.CancelFunc
}

func NewHub(c Collector, interval time.Duration) *Hub {
	return &Hub{c: c, interval: interval, subs: map[chan Stats]struct{}{}}
}

// Subscribe returns a channel of samples (first one delivered immediately) and an unsubscribe func.
func (h *Hub) Subscribe() (<-chan Stats, func()) {
	ch := make(chan Stats, 1)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	if len(h.subs) == 1 {
		ctx, cancel := context.WithCancel(context.Background())
		h.cancel = cancel
		go h.run(ctx)
	}
	h.mu.Unlock()

	var once sync.Once
	return ch, func() {
		once.Do(func() {
			h.mu.Lock()
			delete(h.subs, ch)
			if len(h.subs) == 0 && h.cancel != nil {
				h.cancel()
				h.cancel = nil
			}
			h.mu.Unlock()
		})
	}
}

func (h *Hub) run(ctx context.Context) {
	tick := time.NewTicker(h.interval)
	defer tick.Stop()
	for {
		if st, err := h.c.Stats(); err == nil {
			h.publish(st)
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

func (h *Hub) publish(st Stats) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs {
		select {
		case ch <- st:
		default: // slow consumer: drop this sample
		}
	}
}
