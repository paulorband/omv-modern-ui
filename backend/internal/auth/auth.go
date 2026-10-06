// Package auth implements SPEC-01: login against OMV, local sessions, CSRF guard and rate limit.
package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"omvhome/internal/httpx"
	"omvhome/internal/omv"
)

const (
	CookieName   = "omvhome_session"
	CSRFHeader   = "X-Requested-With"
	CSRFValue    = "omv-home"
	defaultIdle  = 12 * time.Hour
	defaultAbs   = 7 * 24 * time.Hour
	maxLoginFail = 5
	failWindow   = 5 * time.Minute
	blockFor     = 5 * time.Minute
)

type User struct {
	Username string `json:"username"`
	Role     string `json:"role"`
}

// Session is a local session wrapping the user's OMV session.
type Session struct {
	Token    string
	User     User
	OMV      *omv.Session
	created  time.Time
	lastSeen time.Time
}

type Service struct {
	omv     omv.Client
	idle    time.Duration
	abs     time.Duration
	now     func() time.Time
	mu      sync.Mutex
	items   map[string]*Session
	limiter *limiter
}

type Option func(*Service)

// WithClock overrides the time source (tests).
func WithClock(now func() time.Time) Option { return func(s *Service) { s.now = now } }

// WithTTL overrides the idle and absolute session lifetimes (tests).
func WithTTL(idle, abs time.Duration) Option {
	return func(s *Service) { s.idle, s.abs = idle, abs }
}

func NewService(c omv.Client, opts ...Option) *Service {
	s := &Service{omv: c, idle: defaultIdle, abs: defaultAbs, now: time.Now, items: map[string]*Session{}}
	for _, o := range opts {
		o(s)
	}
	s.limiter = newLimiter(maxLoginFail, failWindow, blockFor, func() time.Time { return s.now() })
	return s
}

func newToken() string {
	b := make([]byte, 32) // 256 bits
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// Login authenticates against OMV and opens a local session. key identifies the client for rate limiting.
func (s *Service) Login(ctx context.Context, key, username, password string) (*Session, error) {
	if ok, retry := s.limiter.allow(key); !ok {
		return nil, httpx.TooManyRequests("muitas tentativas; tente novamente em %d s", int(retry.Seconds())+1)
	}
	oSess, err := s.omv.Login(ctx, username, password)
	if err != nil {
		if errors.Is(err, omv.ErrInvalidCredentials) {
			s.limiter.fail(key)
			return nil, httpx.Unauthorized("usuário ou senha inválidos")
		}
		return nil, httpx.Upstream("OMV indisponível")
	}
	if !oSess.IsAdmin() {
		_ = s.omv.Logout(ctx, oSess)
		return nil, httpx.Forbidden("apenas administradores do OMV podem entrar")
	}
	s.limiter.reset(key)

	now := s.now()
	sess := &Session{
		Token:    newToken(),
		User:     User{Username: oSess.Username, Role: oSess.Role},
		OMV:      oSess,
		created:  now,
		lastSeen: now,
	}
	s.mu.Lock()
	s.items[sess.Token] = sess
	s.mu.Unlock()
	return sess, nil
}

// Lookup returns the live session for token and slides its idle timer.
func (s *Service) Lookup(token string) (*Session, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.items[token]
	if !ok {
		return nil, false
	}
	now := s.now()
	if now.Sub(sess.lastSeen) > s.idle || now.Sub(sess.created) > s.abs {
		delete(s.items, token)
		return nil, false
	}
	sess.lastSeen = now
	return sess, true
}

// Logout drops the local session and closes the OMV one (best effort).
func (s *Service) Logout(ctx context.Context, token string) {
	s.mu.Lock()
	sess, ok := s.items[token]
	delete(s.items, token)
	s.mu.Unlock()
	if ok {
		if err := s.omv.Logout(ctx, sess.OMV); err != nil {
			slog.Warn("omv logout failed", "err", err)
		}
	}
}

// Call invokes an OMV RPC as the session's user. If OMV reports the session as expired, the
// local session is dropped and an unauthorized error is returned (R01.8). Other OMV failures
// map to upstream_error.
func (s *Service) Call(ctx context.Context, sess *Session, service, method string, params, out any) error {
	err := s.omv.Call(ctx, sess.OMV, service, method, params, out)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, omv.ErrSessionExpired):
		s.mu.Lock()
		delete(s.items, sess.Token)
		s.mu.Unlock()
		return httpx.Unauthorized("sessão expirada")
	default:
		slog.Warn("omv call failed", "rpc", service+"."+method, "err", err)
		return httpx.Upstream("falha ao consultar o OMV (%s.%s)", service, method)
	}
}

type ctxKey struct{}

// FromContext returns the session placed by Require.
func FromContext(ctx context.Context) *Session {
	s, _ := ctx.Value(ctxKey{}).(*Session)
	return s
}

// Require rejects requests without a valid session cookie.
func (s *Service) Require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(CookieName)
		if err != nil {
			httpx.WriteError(w, httpx.Unauthorized("não autenticado"))
			return
		}
		sess, ok := s.Lookup(c.Value)
		if !ok {
			httpx.WriteError(w, httpx.Unauthorized("sessão inválida ou expirada"))
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, sess)))
	})
}

// CSRFGuard requires the X-Requested-With header on mutating requests (R00.3).
func CSRFGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
		default:
			if r.Header.Get(CSRFHeader) != CSRFValue {
				httpx.WriteError(w, httpx.Forbidden("cabeçalho %s ausente", CSRFHeader))
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func clientKey(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func isSecure(r *http.Request) bool {
	return r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
}

func (s *Service) setCookie(w http.ResponseWriter, r *http.Request, token string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name: CookieName, Value: token, Path: "/", HttpOnly: true, Secure: isSecure(r),
		SameSite: http.SameSiteStrictMode, MaxAge: maxAge,
	})
}

// LoginHandler handles POST /auth/login.
func (s *Service) LoginHandler() httpx.Handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		var in struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if err := httpx.Decode(r, &in); err != nil {
			return err
		}
		if in.Username == "" || in.Password == "" {
			return httpx.BadRequest("informe usuário e senha")
		}
		sess, err := s.Login(r.Context(), clientKey(r), in.Username, in.Password)
		if err != nil {
			return err
		}
		s.setCookie(w, r, sess.Token, int(s.abs.Seconds()))
		httpx.JSON(w, http.StatusOK, map[string]any{"user": sess.User})
		return nil
	}
}

// LogoutHandler handles POST /auth/logout; it is idempotent.
func (s *Service) LogoutHandler() httpx.Handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		if c, err := r.Cookie(CookieName); err == nil {
			s.Logout(r.Context(), c.Value)
		}
		s.setCookie(w, r, "", -1)
		w.WriteHeader(http.StatusNoContent)
		return nil
	}
}

// MeHandler handles GET /auth/me (must be wrapped by Require).
func (s *Service) MeHandler() httpx.Handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		httpx.JSON(w, http.StatusOK, map[string]any{"user": FromContext(r.Context()).User})
		return nil
	}
}

// KeepAlive keeps the OMV sessions of active users from timing out on OMV's own, much shorter, idle
// limit (SPEC-01 R01.11): every `every` it makes a cheap call for each session used within
// activeWithin. Sessions OMV declares expired are dropped, so the UI goes back to the login page.
func (s *Service) KeepAlive(ctx context.Context, every, activeWithin time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.PingActive(ctx, activeWithin)
		}
	}
}

// PingActive is one KeepAlive round.
func (s *Service) PingActive(ctx context.Context, activeWithin time.Duration) {
	s.mu.Lock()
	now := s.now()
	var active []*Session
	for _, sess := range s.items {
		if now.Sub(sess.lastSeen) <= activeWithin && now.Sub(sess.lastSeen) <= s.idle && now.Sub(sess.created) <= s.abs {
			active = append(active, sess)
		}
	}
	s.mu.Unlock()

	for _, sess := range active {
		err := s.omv.Call(ctx, sess.OMV, "Config", "isDirty", nil, nil)
		switch {
		case err == nil:
		case errors.Is(err, omv.ErrSessionExpired):
			s.mu.Lock()
			delete(s.items, sess.Token)
			s.mu.Unlock()
			slog.Info("sessão do OMV expirada; sessão local descartada", "user", sess.User.Username)
		default:
			slog.Debug("keep-alive do OMV falhou", "user", sess.User.Username, "err", err)
		}
	}
}
