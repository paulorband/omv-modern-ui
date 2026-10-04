package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"omvhome/internal/auth"
	"omvhome/internal/omv"
)

// trackingOMV wraps the mock to record logouts.
type trackingOMV struct {
	*omv.Mock
	mu      sync.Mutex
	logouts int
}

func (t *trackingOMV) Logout(ctx context.Context, s *omv.Session) error {
	t.mu.Lock()
	t.logouts++
	t.mu.Unlock()
	return t.Mock.Logout(ctx, s)
}

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func newTestServer(t *testing.T, opts ...auth.Option) (http.Handler, *trackingOMV) {
	t.Helper()
	tr := &trackingOMV{Mock: omv.NewMock()}
	return New(Deps{Auth: auth.NewService(tr, opts...)}), tr
}

func do(h http.Handler, method, path, body string, cookie *http.Cookie, xhr bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.RemoteAddr = "10.0.0.1:1234"
	if xhr {
		req.Header.Set(auth.CSRFHeader, auth.CSRFValue)
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func sessionCookie(rec *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == auth.CookieName && c.Value != "" {
			return c
		}
	}
	return nil
}

func login(t *testing.T, h http.Handler) *http.Cookie {
	t.Helper()
	rec := do(h, "POST", "/api/v1/auth/login", `{"username":"admin","password":"admin"}`, nil, true)
	c := sessionCookie(rec)
	if rec.Code != 200 || c == nil {
		t.Fatalf("login failed: %d %s", rec.Code, rec.Body)
	}
	return c
}

func TestLoginAdminSetsCookie(t *testing.T) { // A01.1
	h, _ := newTestServer(t)
	rec := do(h, "POST", "/api/v1/auth/login", `{"username":"admin","password":"admin"}`, nil, true)
	c := sessionCookie(rec)
	if rec.Code != 200 || c == nil {
		t.Fatalf("got %d, cookie=%v", rec.Code, c)
	}
	if !c.HttpOnly || c.SameSite != http.SameSiteStrictMode {
		t.Errorf("cookie must be HttpOnly+SameSite=Strict: %+v", c)
	}
	if len(c.Value) < 40 {
		t.Errorf("token too short: %d", len(c.Value))
	}
	if !strings.Contains(rec.Body.String(), `"role":"admin"`) {
		t.Errorf("body: %s", rec.Body)
	}
}

func TestLoginInvalidCredentials(t *testing.T) { // A01.2
	h, _ := newTestServer(t)
	rec := do(h, "POST", "/api/v1/auth/login", `{"username":"admin","password":"nope"}`, nil, true)
	if rec.Code != 401 || sessionCookie(rec) != nil {
		t.Fatalf("got %d cookie=%v", rec.Code, sessionCookie(rec))
	}
}

func TestLoginNonAdminForbiddenAndLoggedOut(t *testing.T) { // A01.3
	h, tr := newTestServer(t)
	rec := do(h, "POST", "/api/v1/auth/login", `{"username":"user","password":"user"}`, nil, true)
	if rec.Code != 403 || sessionCookie(rec) != nil {
		t.Fatalf("got %d", rec.Code)
	}
	if tr.logouts != 1 {
		t.Errorf("OMV Session.logout calls = %d, want 1", tr.logouts)
	}
}

func TestProtectedRoute(t *testing.T) { // A01.4
	h, _ := newTestServer(t)
	if rec := do(h, "GET", "/api/v1/auth/me", "", nil, false); rec.Code != 401 {
		t.Fatalf("no cookie: got %d", rec.Code)
	}
	c := login(t, h)
	rec := do(h, "GET", "/api/v1/auth/me", "", c, false)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"username":"admin"`) {
		t.Fatalf("with cookie: %d %s", rec.Code, rec.Body)
	}
}

func TestLogoutInvalidatesSession(t *testing.T) { // A01.5
	h, tr := newTestServer(t)
	c := login(t, h)
	if rec := do(h, "POST", "/api/v1/auth/logout", "", c, true); rec.Code != 204 {
		t.Fatalf("logout: %d", rec.Code)
	}
	if rec := do(h, "GET", "/api/v1/auth/me", "", c, false); rec.Code != 401 {
		t.Fatalf("after logout: got %d", rec.Code)
	}
	if tr.logouts != 1 {
		t.Errorf("OMV logouts = %d", tr.logouts)
	}
}

func TestMutationRequiresHeader(t *testing.T) { // A01.6
	h, _ := newTestServer(t)
	rec := do(h, "POST", "/api/v1/auth/login", `{"username":"admin","password":"admin"}`, nil, false)
	if rec.Code != 403 {
		t.Fatalf("got %d", rec.Code)
	}
}

func TestSessionIdleExpiry(t *testing.T) { // A01.7
	ck := &clock{t: time.Date(2026, 10, 4, 8, 0, 0, 0, time.UTC)}
	h, _ := newTestServer(t, auth.WithClock(ck.now))
	c := login(t, h)

	ck.t = ck.t.Add(11 * time.Hour) // still valid, slides the idle timer
	if rec := do(h, "GET", "/api/v1/auth/me", "", c, false); rec.Code != 200 {
		t.Fatalf("11h: got %d", rec.Code)
	}
	ck.t = ck.t.Add(13 * time.Hour) // idle > 12h
	if rec := do(h, "GET", "/api/v1/auth/me", "", c, false); rec.Code != 401 {
		t.Fatalf("idle expiry: got %d", rec.Code)
	}
}

func TestSessionAbsoluteExpiry(t *testing.T) {
	ck := &clock{t: time.Date(2026, 10, 4, 8, 0, 0, 0, time.UTC)}
	h, _ := newTestServer(t, auth.WithClock(ck.now))
	c := login(t, h)
	for i := 0; i < 8; i++ { // keep it active across 8 days
		ck.t = ck.t.Add(23 * time.Hour)
		rec := do(h, "GET", "/api/v1/auth/me", "", c, false)
		if ck.t.Sub(time.Date(2026, 10, 4, 8, 0, 0, 0, time.UTC)) > 7*24*time.Hour && rec.Code != 401 {
			t.Fatalf("absolute TTL not enforced at %v: %d", ck.t, rec.Code)
		}
	}
}

func TestLoginRateLimit(t *testing.T) { // R01.9
	h, _ := newTestServer(t)
	for i := 0; i < 5; i++ {
		if rec := do(h, "POST", "/api/v1/auth/login", `{"username":"admin","password":"x"}`, nil, true); rec.Code != 401 {
			t.Fatalf("attempt %d: %d", i, rec.Code)
		}
	}
	// even correct credentials are blocked now
	if rec := do(h, "POST", "/api/v1/auth/login", `{"username":"admin","password":"admin"}`, nil, true); rec.Code != 429 {
		t.Fatalf("expected 429, got %d", rec.Code)
	}
}

func TestExpiredOMVSessionInvalidatesLocal(t *testing.T) { // R01.8
	tr := &trackingOMV{Mock: omv.NewMock()}
	svc := auth.NewService(tr)
	sess, err := svc.Login(context.Background(), "k", "admin", "admin")
	if err != nil {
		t.Fatal(err)
	}
	_ = tr.Mock.Logout(context.Background(), sess.OMV) // OMV drops the session behind our back
	if err := svc.Call(context.Background(), sess, "Any", "method", nil, nil); err == nil || !strings.Contains(err.Error(), "unauthorized") {
		t.Fatalf("want unauthorized, got %v", err)
	}
	if _, ok := svc.Lookup(sess.Token); ok {
		t.Fatal("local session should be gone")
	}
}

func TestUnknownAPIRouteIsJSON404(t *testing.T) {
	h, _ := newTestServer(t)
	rec := do(h, "GET", "/api/v1/nope", "", nil, false)
	if rec.Code != 404 || !strings.Contains(rec.Body.String(), `"code":"not_found"`) {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
}
