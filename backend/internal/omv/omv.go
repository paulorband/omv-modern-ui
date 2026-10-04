// Package omv is the only place that knows how to talk to the OpenMediaVault JSON-RPC API.
// Everything else depends on the Client interface so OMV version differences stay contained.
package omv

import (
	"context"
	"errors"
	"fmt"
)

// Session is an authenticated OMV session owned by one user (the PHPSESSID cookie value).
type Session struct {
	ID       string
	Username string
	Role     string
}

// IsAdmin reports whether the OMV user has the administrator role.
func (s *Session) IsAdmin() bool { return s.Role == "admin" }

var (
	// ErrInvalidCredentials means OMV refused the username/password.
	ErrInvalidCredentials = errors.New("omv: invalid credentials")
	// ErrSessionExpired means the OMV session is no longer valid.
	ErrSessionExpired = errors.New("omv: session expired")
)

// RPCError is an error returned by an OMV RPC method.
type RPCError struct {
	Code    int
	Message string
}

func (e *RPCError) Error() string { return fmt.Sprintf("omv rpc error %d: %s", e.Code, e.Message) }

// Client calls OMV on behalf of a user session.
type Client interface {
	Login(ctx context.Context, username, password string) (*Session, error)
	Logout(ctx context.Context, s *Session) error
	// Call invokes service.method; the "response" payload is decoded into out (may be nil).
	Call(ctx context.Context, s *Session, service, method string, params, out any) error
}
