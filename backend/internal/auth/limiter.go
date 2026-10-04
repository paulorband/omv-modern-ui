package auth

import (
	"sync"
	"time"
)

// limiter blocks a key for `block` after `max` failures within `window` (R00.6).
type limiter struct {
	max    int
	window time.Duration
	block  time.Duration
	now    func() time.Time
	mu     sync.Mutex
	m      map[string]*entry
}

type entry struct {
	fails        []time.Time
	blockedUntil time.Time
}

func newLimiter(max int, window, block time.Duration, now func() time.Time) *limiter {
	return &limiter{max: max, window: window, block: block, now: now, m: map[string]*entry{}}
}

func (l *limiter) allow(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	e := l.m[key]
	if e == nil {
		return true, 0
	}
	if now := l.now(); now.Before(e.blockedUntil) {
		return false, e.blockedUntil.Sub(now)
	}
	return true, 0
}

func (l *limiter) fail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	e := l.m[key]
	if e == nil {
		e = &entry{}
		l.m[key] = e
	}
	now := l.now()
	kept := e.fails[:0]
	for _, t := range e.fails {
		if now.Sub(t) <= l.window {
			kept = append(kept, t)
		}
	}
	e.fails = append(kept, now)
	if len(e.fails) >= l.max {
		e.blockedUntil = now.Add(l.block)
		e.fails = nil
	}
}

func (l *limiter) reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.m, key)
}
