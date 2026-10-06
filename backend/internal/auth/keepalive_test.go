package auth

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"omvhome/internal/omv"
)

// stubOMV logs calls and can fail them per session id.
type stubOMV struct {
	mu    sync.Mutex
	calls []string
	errs  map[string]error
}

func (s *stubOMV) Login(_ context.Context, u, _ string) (*omv.Session, error) {
	return &omv.Session{ID: "omv-" + u, Username: u, Role: "admin"}, nil
}
func (s *stubOMV) Logout(context.Context, *omv.Session) error { return nil }
func (s *stubOMV) Call(_ context.Context, sess *omv.Session, service, method string, _, _ any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, sess.ID+":"+service+"."+method)
	return s.errs[sess.ID]
}

func TestPingActiveKeepsOnlyActiveSessionsAlive(t *testing.T) { // A01.10
	clock := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	stub := &stubOMV{errs: map[string]error{}}
	svc := NewService(stub, WithClock(func() time.Time { return clock }))
	ctx := context.Background()

	active, _ := svc.Login(ctx, "k", "alice", "x")
	idle, _ := svc.Login(ctx, "k", "bob", "x")
	clock = clock.Add(30 * time.Minute)
	if _, ok := svc.Lookup(active.Token); !ok { // alice is used just now, bob 30 min ago
		t.Fatal("alice lost her session")
	}

	svc.PingActive(ctx, 10*time.Minute)
	if len(stub.calls) != 1 || stub.calls[0] != "omv-alice:Config.isDirty" {
		t.Fatalf("only the active session may be pinged: %v", stub.calls)
	}
	if _, ok := svc.Lookup(idle.Token); !ok {
		t.Error("an idle session is not dropped by the keep-alive, it only stops being refreshed")
	}
}

func TestPingActiveDropsSessionsOMVExpired(t *testing.T) { // A01.10
	stub := &stubOMV{errs: map[string]error{"omv-alice": omv.ErrSessionExpired, "omv-bob": errors.New("connection refused")}}
	svc := NewService(stub)
	ctx := context.Background()
	alice, _ := svc.Login(ctx, "k", "alice", "x")
	bob, _ := svc.Login(ctx, "k", "bob", "x")

	svc.PingActive(ctx, time.Minute)
	if _, ok := svc.Lookup(alice.Token); ok {
		t.Error("expired OMV session must drop the local one")
	}
	if _, ok := svc.Lookup(bob.Token); !ok {
		t.Error("other failures (OMV restarting…) must not log the user out")
	}
}

func TestKeepAliveStopsWithContext(t *testing.T) {
	stub := &stubOMV{}
	svc := NewService(stub)
	svc.Login(context.Background(), "k", "alice", "x")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { svc.KeepAlive(ctx, 5*time.Millisecond, time.Minute); close(done) }()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		stub.mu.Lock()
		n := len(stub.calls)
		stub.mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("KeepAlive did not stop")
	}
	if len(stub.calls) == 0 {
		t.Error("KeepAlive never pinged")
	}
}
