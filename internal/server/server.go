// Package server exposes the companion HTTP surface: the local REST API and the
// embedded web application.
//
// Phase 0 ships the skeleton the later phases hang handlers on: the router and
// its middleware chain, the unauthenticated health probe, bearer-token
// authentication with RFC 7807 problem responses, security headers and the SPA
// fallback that serves the embedded frontend. Items, boards, git operations and
// the WebSocket event stream arrive with the companion CLI in Phase 2.
//
// All domain logic lives in internal/core; this package only adapts it to HTTP.
package server

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// DefaultPort is the loopback port the companion listens on.
const DefaultPort = 7317

// DefaultBind is the interface the companion binds to. Binding anywhere else is
// possible but is a deliberate, documented choice: the token is not a security
// boundary against other users of the machine.
const DefaultBind = "127.0.0.1"

// apiPrefix is the versioned base path of the REST API.
const apiPrefix = "/api/v1"

// problemBase is the namespace of the problem types returned by the API.
const problemBase = "https://git-in-track.dev/problems/"

// requestTimeout bounds a single request. Long operations (full reindex, git
// fetch) get their own deadline once they exist.
const requestTimeout = 30 * time.Second

// Options configures a Server.
type Options struct {
	// Bind is the interface to listen on, DefaultBind when empty.
	Bind string
	// Port is the TCP port, DefaultPort when zero.
	Port int
	// Token is the bearer token required by every route but /health. An empty
	// token disables authentication, which New refuses on a non-loopback bind.
	Token string
	// Dev relaxes CORS for the Vite dev server and logs at debug level.
	Dev bool
	// OpenBrowser asks the caller to open the UI once the server is listening.
	OpenBrowser bool
	// IdleTimeout stops the server after this long without a request. Zero
	// disables the behavior.
	IdleTimeout time.Duration
	// Version, Commit and Mode are reported by /health and /capabilities.
	Version string
	Commit  string
	Mode    string
	// UI is the file system holding the built frontend. When nil, or when it
	// has no index.html, the SPA handler explains how to build it.
	UI fs.FS
	// Logger receives request and lifecycle logs. Defaults to slog.Default().
	Logger *slog.Logger
}

// Server owns the router and the HTTP listener.
type Server struct {
	opts    Options
	router  chi.Router
	log     *slog.Logger
	started time.Time

	// mu guards addr, which changes once when the listener resolves a
	// wildcard port and is read concurrently by callers printing the URL.
	mu   sync.RWMutex
	addr string
}

// modeCompanion is the operating mode of the local companion process, as
// opposed to the browser-only mode that runs the same core in WebAssembly.
const modeCompanion = "companion"

// New builds a Server. It returns an error when the options are inconsistent,
// for example authentication disabled on a non-loopback bind.
func New(opts Options) (*Server, error) {
	if opts.Bind == "" {
		opts.Bind = DefaultBind
	}
	if opts.Port == 0 {
		opts.Port = DefaultPort
	}
	if opts.Version == "" {
		opts.Version = "dev"
	}
	if opts.Mode == "" {
		opts.Mode = modeCompanion
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.Token == "" && !isLoopback(opts.Bind) {
		return nil, fmt.Errorf("refusing to serve %s without a token: authentication may only be disabled on loopback", opts.Bind)
	}

	s := &Server{
		opts:    opts,
		log:     opts.Logger,
		started: time.Now(),
		addr:    net.JoinHostPort(opts.Bind, fmt.Sprint(opts.Port)),
	}
	s.router = s.routes()
	return s, nil
}

// GenerateToken returns a fresh bearer token: 32 random bytes, base64url.
func GenerateToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// Addr returns the host:port the server listens on. After Start it reports the
// resolved address, which differs from the requested one when the port was 0.
func (s *Server) Addr() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.addr
}

// URL returns the address a browser should open.
func (s *Server) URL() string { return "http://" + s.Addr() }

// Handler returns the router, which is what the tests exercise.
func (s *Server) Handler() http.Handler { return s.router }

// Start listens and serves until ctx is canceled, then shuts down gracefully.
func (s *Server) Start(ctx context.Context) error {
	var lc net.ListenConfig
	listener, err := lc.Listen(ctx, "tcp", s.Addr())
	if err != nil {
		return fmt.Errorf("listen on %s: %w", s.Addr(), err)
	}
	s.mu.Lock()
	s.addr = listener.Addr().String()
	s.mu.Unlock()

	srv := &http.Server{
		Handler:           s.router,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		ErrorLog:          slog.NewLogLogger(s.log.Handler(), slog.LevelWarn),
	}

	errCh := make(chan error, 1)
	go func() {
		s.log.Info("listening", "url", s.URL(), "mode", s.opts.Mode, "version", s.opts.Version)
		if err := srv.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("serve: %w", err)
			return
		}
		errCh <- nil
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	<-errCh
	return nil
}

// routes composes the router.
func (s *Server) routes() chi.Router {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	// middleware.RealIP is deliberately absent: it trusts client-controlled
	// forwarding headers (GHSA-3fxj-6jh8-hvhx) and the companion listens on
	// loopback, where the peer address is already the truth.
	r.Use(s.requestLogger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(requestTimeout))
	r.Use(securityHeaders)

	r.Route(apiPrefix, func(api chi.Router) {
		api.Get("/health", s.handleHealth)
		api.Group(func(private chi.Router) {
			private.Use(s.bearerAuth)
			private.Get("/capabilities", s.handleCapabilities)
		})
		api.NotFound(s.handleAPINotFound)
		api.MethodNotAllowed(s.handleAPINotFound)
	})

	r.NotFound(s.spaHandler())
	return r
}

// handleHealth is the unauthenticated liveness probe.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, r, http.StatusOK, map[string]any{
		"status":        "ok",
		"version":       s.opts.Version,
		"mode":          s.opts.Mode,
		"uptimeSeconds": int64(time.Since(s.started).Seconds()),
	})
}

// handleCapabilities reports what this build can do, so that the web app can
// upgrade itself from browser-only mode to companion mode.
func (s *Server) handleCapabilities(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, r, http.StatusOK, map[string]any{
		"version": s.opts.Version,
		"commit":  s.opts.Commit,
		"schema":  "v1",
		"mode":    s.opts.Mode,
		"ui":      s.uiState(),
		"features": map[string]bool{
			// Phase 0 ships the skeleton only; each feature is switched on by
			// the phase that implements it.
			"watcher": false,
			"git":     false,
			"mcp":     false,
			"write":   false,
		},
	})
}

// handleAPINotFound keeps unknown API paths inside the problem+json contract
// instead of falling through to the SPA.
func (s *Server) handleAPINotFound(w http.ResponseWriter, r *http.Request) {
	writeProblem(w, r, http.StatusNotFound, "not_found", "Not found",
		fmt.Sprintf("%s %s is not a route of this API.", r.Method, r.URL.Path))
}

// uiState reports whether the embedded frontend is present.
func (s *Server) uiState() string {
	if s.opts.UI == nil {
		return "absent"
	}
	if _, err := fs.Stat(s.opts.UI, "index.html"); err != nil {
		return "absent"
	}
	return "embedded"
}

// bearerAuth guards every route but the health probe.
func (s *Server) bearerAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.opts.Token == "" {
			// Authentication is disabled; New has already refused this on a
			// non-loopback bind.
			next.ServeHTTP(w, r)
			return
		}
		if !tokenMatches(s.opts.Token, presentedToken(r)) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="gintrack"`)
			writeProblem(w, r, http.StatusUnauthorized, "unauthorized", "Unauthorized",
				"A bearer token is required. Start the server with `gintrack serve` and use the token it prints.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// presentedToken extracts the token from the Authorization header or, for
// browser clients that cannot set headers on a WebSocket, the query string.
func presentedToken(r *http.Request) string {
	if h := r.Header.Get("Authorization"); h != "" {
		if token, ok := strings.CutPrefix(h, "Bearer "); ok {
			return strings.TrimSpace(token)
		}
		return ""
	}
	return r.URL.Query().Get("token")
}

// tokenMatches compares two tokens in constant time.
func tokenMatches(want, got string) bool {
	return subtle.ConstantTimeCompare([]byte(want), []byte(got)) == 1
}

// securityHeaders sets the headers that make a local origin safe to open in a
// browser: no sniffing, no framing, no referrer leakage, and a content security
// policy that keeps the app self-contained.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Content-Security-Policy",
			"default-src 'self'; img-src 'self' data: blob:; style-src 'self' 'unsafe-inline'; "+
				"script-src 'self' 'wasm-unsafe-eval'; connect-src 'self' ws: wss:; object-src 'none'; base-uri 'none'")
		next.ServeHTTP(w, r)
	})
}

// requestLogger logs one line per request at debug level, or at warn level for
// server errors.
func (s *Server) requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		attrs := []any{
			"method", r.Method,
			"path", r.URL.Path,
			"status", ww.Status(),
			"bytes", ww.BytesWritten(),
			"duration", time.Since(start).String(),
			"requestId", middleware.GetReqID(r.Context()),
		}
		if ww.Status() >= http.StatusInternalServerError {
			s.log.Warn("request", attrs...)
			return
		}
		s.log.Debug("request", attrs...)
	})
}

// writeJSON writes a JSON response with the request id echoed back.
func writeJSON(w http.ResponseWriter, r *http.Request, status int, payload any) {
	if id := middleware.GetReqID(r.Context()); id != "" {
		w.Header().Set("X-Request-Id", id)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		// The response is already on the wire; there is nothing left to do but
		// let the request logger record the truncated body.
		_ = err
	}
}

// problem is an RFC 7807 problem document, with the machine-readable `code`
// clients switch on (docs/07 section 5.4).
type problem struct {
	Type      string `json:"type"`
	Title     string `json:"title"`
	Status    int    `json:"status"`
	Detail    string `json:"detail,omitempty"`
	Instance  string `json:"instance,omitempty"`
	Code      string `json:"code"`
	RequestID string `json:"requestId,omitempty"`
}

// writeProblem writes an application/problem+json response.
func writeProblem(w http.ResponseWriter, r *http.Request, status int, code, title, detail string) {
	id := middleware.GetReqID(r.Context())
	if id != "" {
		w.Header().Set("X-Request-Id", id)
	}
	w.Header().Set("Content-Type", "application/problem+json; charset=utf-8")
	w.WriteHeader(status)
	body := problem{
		Type:      problemBase + strings.ReplaceAll(code, "_", "-"),
		Title:     title,
		Status:    status,
		Detail:    detail,
		Instance:  r.URL.Path,
		Code:      code,
		RequestID: id,
	}
	if err := json.NewEncoder(w).Encode(body); err != nil {
		_ = err
	}
}

// isLoopback reports whether a bind address is a loopback interface.
func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
