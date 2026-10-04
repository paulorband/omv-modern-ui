package omv

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
)

// MockHandler answers one mocked RPC method.
type MockHandler func(params json.RawMessage) (any, error)

// Mock is an in-memory OMV used with --mock and in tests. Accounts: admin/admin (administrator)
// and user/user (regular user). These credentials exist only in mock mode.
type Mock struct {
	mu       sync.Mutex
	sessions map[string]*Session
	handlers map[string]MockHandler
}

func NewMock() *Mock {
	return &Mock{sessions: map[string]*Session{}, handlers: map[string]MockHandler{}}
}

// Handle registers a handler for "Service.method".
func (m *Mock) Handle(name string, h MockHandler) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.handlers[name] = h
}

func (m *Mock) Login(_ context.Context, username, password string) (*Session, error) {
	var role string
	switch {
	case username == "admin" && password == "admin":
		role = "admin"
	case username == "user" && password == "user":
		role = "user"
	default:
		return nil, ErrInvalidCredentials
	}
	buf := make([]byte, 16)
	_, _ = rand.Read(buf)
	s := &Session{ID: hex.EncodeToString(buf), Username: username, Role: role}
	m.mu.Lock()
	m.sessions[s.ID] = s
	m.mu.Unlock()
	return s, nil
}

func (m *Mock) Logout(_ context.Context, s *Session) error {
	m.mu.Lock()
	delete(m.sessions, s.ID)
	m.mu.Unlock()
	return nil
}

func (m *Mock) Call(_ context.Context, s *Session, service, method string, params, out any) error {
	m.mu.Lock()
	_, ok := m.sessions[s.ID]
	h := m.handlers[service+"."+method]
	m.mu.Unlock()
	if !ok {
		return ErrSessionExpired
	}
	if h == nil {
		return &RPCError{Code: 1, Message: fmt.Sprintf("mock: %s.%s not implemented", service, method)}
	}
	raw, err := json.Marshal(params)
	if err != nil {
		return err
	}
	res, err := h(raw)
	if err != nil {
		return err
	}
	if out == nil || res == nil {
		return nil
	}
	b, err := json.Marshal(res)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}
