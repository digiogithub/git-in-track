// Package server exposes the companion HTTP surface: the local REST API, the
// WebSocket event stream and the embedded web application.
//
// Every repository the companion serves is mounted as an internal/vault.Vault
// over an internal/core/osfs file system, and every endpoint is a thin adapter
// over vault.Dispatch — the same implementation the browser build runs in
// WebAssembly. That is deliberate: the two operating modes answer with the same
// JSON because they run the same code, not because two implementations agree.
//
// The file watcher folds file-system changes into those vaults and the hub fans
// the resulting events out to the connected UIs. Boards, sprints, retrospectives
// and the git surface belong to Phases 3 and 4; their routes exist and answer
// with a `not_implemented` problem.
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

	"github.com/digiogithub/git-in-track/internal/config"
	"github.com/digiogithub/git-in-track/internal/mcp"
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

	// Repos are the repositories to mount. Each one becomes a vault over the
	// files on disk; a repository that cannot be opened is reported as broken
	// instead of stopping the server.
	Repos []Repo
	// Workspace is the name of the workspace being served, reported by
	// /capabilities and /workspaces.
	Workspace string
	// Watch enables the file watcher and, with it, live updates over the
	// WebSocket.
	Watch bool
	// Debounce is the watcher coalescing window; zero means the watcher default.
	Debounce time.Duration
	// ExtraOrigins are additional browser origins allowed through CORS.
	ExtraOrigins []string
	// NewWatcher builds the watcher. Tests replace it; production leaves it nil
	// and gets the fsnotify-backed one.
	NewWatcher WatcherFactory
	// Now is the clock stamping events and index timestamps. Nil means
	// time.Now.
	Now func() time.Time

	// Git is the git section of the configuration: the backend, whether
	// commit-on-save is on, the message template and the debounce window
	// (docs/06-git-sync.md section 3.3).
	Git config.Git
	// ConfigPath is the configuration file a settings change made through
	// PATCH /api/v1/git/settings is persisted to. Empty keeps such a change in
	// this process only, which is what a test and `serve --repo` want.
	ConfigPath string

	// MCPHTTP mounts the Model Context Protocol server at POST /mcp, behind the
	// same bearer token as the REST API (docs/08-mcp-server.md section 2.2).
	MCPHTTP bool
	// MCPAllowWrite advertises the write tools of that server. Without it the
	// endpoint is read-only and the write tools are absent from tools/list.
	MCPAllowWrite bool
	// MCPAgent is the name agent-authored comments are attributed to. Empty
	// means the default the MCP package picks.
	MCPAgent string
}

// Server owns the router and the HTTP listener.
type Server struct {
	opts    Options
	router  chi.Router
	log     *slog.Logger
	started time.Time
	now     func() time.Time

	// repos holds the mounted repositories and their vaults.
	repos *registry
	// hub fans events out to the connected event streams.
	hub *Hub
	// watch owns the file watcher, when there is one.
	watch watchState
	// git owns the commit-on-save committer and the per-repository backends.
	git *gitState
	// mcp is the Model Context Protocol server mounted at /mcp, nil when the
	// endpoint is disabled.
	mcp *mcp.Server

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

	if opts.Workspace == "" {
		opts.Workspace = config.DefaultWorkspaceName
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}

	s := &Server{
		opts:    opts,
		log:     opts.Logger,
		started: now(),
		now:     now,
		addr:    net.JoinHostPort(opts.Bind, fmt.Sprint(opts.Port)),
	}
	s.repos = newRegistry(opts.Repos, now)
	s.hub = newHub(opts.Workspace, now)
	s.git = newGitState(opts, s.repos, s.log, s.publishCommit)
	for _, m := range s.repos.all() {
		if m.err != nil {
			s.log.Warn("repository mounted with errors", "repo", m.id, "path", m.path, "error", m.err)
		}
	}
	s.mcp = s.newMCPServer(opts)
	s.router = s.routes()
	return s, nil
}

// Repos reports the mounted repositories: their id, their path and the project
// keys they expose. `gintrack serve` prints it as the startup banner.
func (s *Server) Repos() []RepoStatus {
	out := make([]RepoStatus, 0, len(s.repos.all()))
	for _, m := range s.repos.all() {
		status := RepoStatus{ID: m.id, Path: m.path, Role: m.role, Projects: m.projectKeys(), Err: m.err}
		if m.ready() {
			stats := m.vlt.Stats()
			status.Items, status.Pages, status.Comments = stats.Items, stats.Pages, stats.Comments
		}
		out = append(out, status)
	}
	return out
}

// RepoStatus is what a mounted repository looks like to the command line.
type RepoStatus struct {
	ID       string
	Path     string
	Role     string
	Projects []string
	Items    int
	Pages    int
	Comments int
	Err      error
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

	s.startWatch(ctx)
	defer s.stopWatch()
	// A shutdown must not drop an edit that was still inside the debounce
	// window, so the committer is flushed before the listener closes.
	defer s.git.close(context.WithoutCancel(ctx))

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
	r.Use(s.corsMiddleware)
	r.Use(securityHeaders)
	// The request deadline is applied per route rather than globally: the event
	// stream is a long-lived connection, not a request that must finish.
	r.Use(s.timeoutExceptStream)

	r.Route(apiPrefix, s.mountAPI)
	// The MCP endpoint lives outside the REST prefix: /mcp is the path every
	// client configuration expects, and it speaks JSON-RPC, not REST.
	s.mountMCP(r)

	r.NotFound(s.spaHandler())
	r.MethodNotAllowed(s.spaHandler())
	return r
}

// timeoutExceptStream applies the request deadline to everything but the
// WebSocket endpoint.
func (s *Server) timeoutExceptStream(next http.Handler) http.Handler {
	bounded := middleware.Timeout(requestTimeout)(next)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The event stream and the MCP endpoint are long-lived connections, not
		// requests that must finish inside the deadline.
		if r.URL.Path == apiPrefix+"/events" || r.URL.Path == mcpPath || strings.HasPrefix(r.URL.Path, mcpPath+"/") {
			next.ServeHTTP(w, r)
			return
		}
		bounded.ServeHTTP(w, r)
	})
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
	projects := make([]string, 0, len(s.repos.all()))
	for _, m := range s.repos.ready() {
		projects = append(projects, m.projectKeys()...)
	}
	writeJSON(w, r, http.StatusOK, map[string]any{
		"version": s.opts.Version,
		"commit":  s.opts.Commit,
		"schema":  "v1",
		"mode":    s.opts.Mode,
		"ui":      s.uiState(),
		"features": map[string]any{
			"watcher":      s.watching(),
			"nativeIndex":  true,
			"write":        true,
			"search":       "core",
			"renderer":     "client",
			"openInEditor": false,
			// Git: commit-on-save ships with GIT-US-0020; the sync pipeline
			// follows with GIT-US-0021.
			"git":          len(s.git.backends) > 0,
			"gitBackend":   s.git.resolved,
			"gitVersion":   s.git.version,
			"commitOnSave": s.git.enabled(),
			"gitSync":      false,
			"mcpHttp":      s.mcp != nil,
			"mcpWrite":     s.opts.MCPAllowWrite && s.mcp != nil,
			"mcpTools":     s.mcpTools(),
			"boards":       true,
		},
		"limits": map[string]int{
			"maxItemsPerPage": maxItemsPerPage,
			"maxBatchWrite":   50,
			"maxUploadBytes":  maxRequestBody,
		},
		"workspaces":      []string{s.opts.Workspace},
		"activeWorkspace": s.opts.Workspace,
		"repos":           len(s.repos.all()),
		"projects":        projects,
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
	if token := r.URL.Query().Get("token"); token != "" {
		return token
	}
	return subprotocolToken(r)
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
		if isUpgrade(r) {
			// A wrapped writer would hide the hijacker the WebSocket upgrade
			// needs; a long-lived stream is logged when it ends, not by status.
			next.ServeHTTP(w, r)
			s.log.Debug("stream closed", "path", r.URL.Path, "duration", time.Since(start).String())
			return
		}
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

// isUpgrade reports whether a request asks for a protocol upgrade.
func isUpgrade(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket")
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

// isLoopback reports whether a bind address is a loopback interface.
func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
