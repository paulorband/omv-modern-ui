package omv

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// OMV error codes for "not authenticated" / "session timed out". ⚠️ a validar contra OMV 7/8.
const (
	codeSessionNotAuthenticated = 5000
	codeSessionTimeout          = 5001
)

// RPCClient talks to {baseURL}/rpc.php.
type RPCClient struct {
	baseURL string
	hc      *http.Client
}

func NewRPCClient(baseURL string) *RPCClient {
	return &RPCClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		hc:      &http.Client{Timeout: 30 * time.Second},
	}
}

type rpcRequest struct {
	Service string `json:"service"`
	Method  string `json:"method"`
	Params  any    `json:"params"`
	Options any    `json:"options"`
}

type rpcResponse struct {
	Response json.RawMessage `json:"response"`
	Error    *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// do posts one RPC replaying cookies, and returns the envelope plus the cookies OMV set.
func (c *RPCClient) do(ctx context.Context, cookies []*http.Cookie, service, method string, params any) (*rpcResponse, []*http.Cookie, error) {
	if params == nil {
		params = map[string]any{}
	}
	body, err := json.Marshal(rpcRequest{Service: service, Method: method, Params: params})
	if err != nil {
		return nil, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/rpc.php", bytes.NewReader(body))
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	for _, ck := range cookies {
		req.AddCookie(&http.Cookie{Name: ck.Name, Value: ck.Value})
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("omv: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, nil, fmt.Errorf("omv: read body: %w", err)
	}
	var env rpcResponse
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, nil, &httpStatusError{Status: resp.StatusCode, Base: c.baseURL}
	}
	return &env, resp.Cookies(), nil
}

// mergeCookies applies Set-Cookie updates (by name) onto the stored cookies.
func mergeCookies(have, set []*http.Cookie) []*http.Cookie {
	out := append([]*http.Cookie(nil), have...)
	for _, n := range set {
		replaced := false
		for i, h := range out {
			if h.Name == n.Name {
				out[i], replaced = n, true
			}
		}
		if !replaced {
			out = append(out, n)
		}
	}
	return out
}

// Login authenticates. The session cookie name differs between OMV releases, so every cookie
// OMV sets is stored and replayed instead of hard-coding one.
func (c *RPCClient) Login(ctx context.Context, username, password string) (*Session, error) {
	env, cookies, err := c.do(ctx, nil, "Session", "login", map[string]string{"username": username, "password": password})
	if err != nil {
		return nil, err
	}
	if env.Error != nil {
		return nil, ErrInvalidCredentials
	}
	var out struct {
		// OMV 7+ answers {"status":"authenticated"}; older releases used {"authenticated":true}.
		Status        string `json:"status"`
		Authenticated bool   `json:"authenticated"`
		Username      string `json:"username"`
		SessionID     string `json:"sessionid"`
		Permissions   struct {
			Role string `json:"role"`
		} `json:"permissions"`
	}
	if err := json.Unmarshal(env.Response, &out); err != nil || !(out.Status == "authenticated" || out.Authenticated) {
		return nil, ErrInvalidCredentials
	}
	if len(cookies) == 0 {
		return nil, fmt.Errorf("omv: login succeeded but no session cookie was set")
	}
	id := out.SessionID
	if id == "" {
		id = cookies[0].Value
	}
	return &Session{ID: id, Username: out.Username, Role: out.Permissions.Role, Cookies: cookies}, nil
}

func (c *RPCClient) Logout(ctx context.Context, s *Session) error {
	_, _, err := c.do(ctx, s.cookies(), "Session", "logout", nil)
	return err
}

func (c *RPCClient) Call(ctx context.Context, s *Session, service, method string, params, out any) error {
	env, set, err := c.do(ctx, s.cookies(), service, method, params)
	if err != nil {
		var hs *httpStatusError
		if errors.As(err, &hs) && (hs.Status == http.StatusUnauthorized || hs.Status == http.StatusForbidden) {
			return ErrSessionExpired
		}
		return err
	}
	s.updateCookies(set)
	if env.Error != nil {
		if isSessionError(env.Error.Code, env.Error.Message) {
			return ErrSessionExpired
		}
		return &RPCError{Code: env.Error.Code, Message: env.Error.Message}
	}
	if out == nil || len(env.Response) == 0 || string(env.Response) == "null" {
		return nil
	}
	if err := json.Unmarshal(env.Response, out); err != nil {
		return fmt.Errorf("omv: decode %s.%s: %w", service, method, err)
	}
	return nil
}

// httpStatusError is a /rpc.php answer that is not the JSON envelope (HTML login page, proxy error…).
type httpStatusError struct {
	Status int
	Base   string
}

func (e *httpStatusError) Error() string {
	return fmt.Sprintf("omv: unexpected response from %s/rpc.php (HTTP %d): is OMVHOME_OMV_URL pointing at OMV?", e.Base, e.Status)
}

// isSessionError recognizes "not logged in / session timed out" answers: by the known codes and, since
// codes differ between OMV versions, by message (SPEC-01 R01.12).
func isSessionError(code int, msg string) bool {
	if code == codeSessionNotAuthenticated || code == codeSessionTimeout {
		return true
	}
	m := strings.ToLower(msg)
	return strings.Contains(m, "session") && (strings.Contains(m, "not authenticated") || strings.Contains(m, "expired") ||
		strings.Contains(m, "timeout") || strings.Contains(m, "timed out") || strings.Contains(m, "not logged"))
}
