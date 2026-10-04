package omv

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// fakeOMV emulates the /rpc.php envelope.
func fakeOMV(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rpc.php" || r.Method != http.MethodPost {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		var req struct {
			Service, Method string
			Params          map[string]any
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "application/json")
		switch req.Service + "." + req.Method {
		case "Session.login":
			if req.Params["password"] != "secret" {
				_, _ = w.Write([]byte(`{"response":null,"error":{"code":5000,"message":"Incorrect username or password."}}`))
				return
			}
			http.SetCookie(w, &http.Cookie{Name: "X-OPENMEDIAVAULT-SESSIONID", Value: "abc123"})
			_, _ = w.Write([]byte(`{"response":{"username":"admin","status":"authenticated","permissions":{"role":"admin"},"sessionid":"abc123"},"error":null}`))
		case "Test.echo":
			if c, err := r.Cookie("X-OPENMEDIAVAULT-SESSIONID"); err != nil || c.Value != "abc123" {
				_, _ = w.Write([]byte(`{"response":null,"error":{"code":5001,"message":"Session expired."}}`))
				return
			}
			_, _ = w.Write([]byte(`{"response":{"ok":true},"error":null}`))
		case "Test.boom":
			_, _ = w.Write([]byte(`{"response":null,"error":{"code":1,"message":"kaboom"}}`))
		default:
			_, _ = w.Write([]byte(`{"response":null,"error":null}`))
		}
	}))
}

func TestRPCClientLoginAndCall(t *testing.T) {
	srv := fakeOMV(t)
	defer srv.Close()
	c := NewRPCClient(srv.URL)
	ctx := context.Background()

	if _, err := c.Login(ctx, "admin", "wrong"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("want ErrInvalidCredentials, got %v", err)
	}
	s, err := c.Login(ctx, "admin", "secret")
	if err != nil {
		t.Fatal(err)
	}
	if s.ID != "abc123" || !s.IsAdmin() || s.Username != "admin" {
		t.Fatalf("session: %+v", s)
	}

	var out struct{ OK bool }
	if err := c.Call(ctx, s, "Test", "echo", nil, &out); err != nil || !out.OK {
		t.Fatalf("echo: %v %+v", err, out)
	}
	var rpcErr *RPCError
	if err := c.Call(ctx, s, "Test", "boom", nil, nil); !errors.As(err, &rpcErr) || rpcErr.Message != "kaboom" {
		t.Fatalf("boom: %v", err)
	}
	if err := c.Call(ctx, &Session{ID: "stale", Cookies: []*http.Cookie{{Name: "X-OPENMEDIAVAULT-SESSIONID", Value: "stale"}}}, "Test", "echo", nil, nil); !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("stale session: %v", err)
	}
}
