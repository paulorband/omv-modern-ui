package omv

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// OMV error codes for "not authenticated" / "session timed out". ⚠️ a validar contra OMV 7.
const (
	codeSessionNotAuthenticated = 5000
	codeSessionTimeout          = 5001
)

const sessionCookie = "PHPSESSID"

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

// do posts one RPC and returns the decoded envelope plus any new session cookie.
func (c *RPCClient) do(ctx context.Context, sessionID, service, method string, params any) (*rpcResponse, string, error) {
	if params == nil {
		params = map[string]any{}
	}
	body, err := json.Marshal(rpcRequest{Service: service, Method: method, Params: params})
	if err != nil {
		return nil, "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/rpc.php", bytes.NewReader(body))
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if sessionID != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sessionID})
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("omv: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, "", fmt.Errorf("omv: read body: %w", err)
	}
	var env rpcResponse
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, "", fmt.Errorf("omv: unexpected response (HTTP %d)", resp.StatusCode)
	}
	newID := ""
	for _, ck := range resp.Cookies() {
		if ck.Name == sessionCookie {
			newID = ck.Value
		}
	}
	return &env, newID, nil
}

func (c *RPCClient) Login(ctx context.Context, username, password string) (*Session, error) {
	env, id, err := c.do(ctx, "", "Session", "login", map[string]string{"username": username, "password": password})
	if err != nil {
		return nil, err
	}
	if env.Error != nil {
		return nil, ErrInvalidCredentials
	}
	var out struct {
		Authenticated bool   `json:"authenticated"`
		Username      string `json:"username"`
		Permissions   struct {
			Role string `json:"role"`
		} `json:"permissions"`
	}
	if err := json.Unmarshal(env.Response, &out); err != nil || !out.Authenticated || id == "" {
		return nil, ErrInvalidCredentials
	}
	return &Session{ID: id, Username: out.Username, Role: out.Permissions.Role}, nil
}

func (c *RPCClient) Logout(ctx context.Context, s *Session) error {
	_, _, err := c.do(ctx, s.ID, "Session", "logout", nil)
	return err
}

func (c *RPCClient) Call(ctx context.Context, s *Session, service, method string, params, out any) error {
	env, _, err := c.do(ctx, s.ID, service, method, params)
	if err != nil {
		return err
	}
	if env.Error != nil {
		if env.Error.Code == codeSessionNotAuthenticated || env.Error.Code == codeSessionTimeout {
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
