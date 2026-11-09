package hub

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/digiogithub/git-in-track/internal/config"
	"github.com/digiogithub/git-in-track/internal/gitops"
	"github.com/digiogithub/git-in-track/internal/mcp"
	"github.com/digiogithub/git-in-track/internal/server"
	"github.com/digiogithub/git-in-track/internal/vault"
)

// Mode is the server mode the hub reports, next to the companion's.
const Mode = "hub"

// Deps are what the binary hands the hub.
type Deps struct {
	// UI is the built web app (web.DistFS in production).
	UI fs.FS
	// Version and Commit are reported by the server.
	Version, Commit string
	// Logger defaults to slog.Default.
	Logger *slog.Logger
	// Git is the git executable used for clones; empty means "git".
	Git string
	// RevalidateEvery is how often an event stream re-checks its token; zero
	// means the server default. Tests shorten it.
	RevalidateEvery time.Duration
}

// Hub is a configured server plus the loop that keeps its clones in step with
// the forge.
type Hub struct {
	cfg    *Config
	srv    *server.Server
	log    *slog.Logger
	syncer *syncLoop
}

// New prepares the data directory (ssh configuration and clones) and builds the
// server over the clones. It listens on nothing until Run.
func New(ctx context.Context, cfg *Config, deps Deps) (*Hub, error) {
	if cfg == nil {
		return nil, errors.New("hub: nil configuration")
	}
	log := deps.Logger
	if log == nil {
		log = slog.Default()
	}
	if err := Prepare(ctx, cfg, deps.Git); err != nil {
		return nil, err
	}
	h := &Hub{cfg: cfg, log: log}
	h.syncer = newSyncLoop(time.Duration(cfg.PullInterval), commitPushDelay, log)

	opts := serverOptions(cfg, deps, log, h.syncer.trigger)
	opts.RevalidateEvery = deps.RevalidateEvery
	if cfg.Auth.PrincipalsFile != "" {
		auth, err := NewAuthenticator(cfg.Auth.PrincipalsFile, log)
		if err != nil {
			return nil, err
		}
		if opts.Token != "" {
			log.Warn("auth.principalsFile is set: the single token is ignored; every request needs a principal's token")
			opts.Token = ""
		}
		opts.Authenticate = auth.Authenticate
		space := func() *vault.Workspace { return h.srv.Workspace() }
		guard := func(next mcp.Dispatcher, mount string) *Guard {
			return NewGuard(next, space, mount, auth.GrantsOf, log)
		}
		opts.Guard = func(next mcp.Dispatcher, mount string) mcp.Dispatcher { return guard(next, mount) }
		opts.SearchScope = guard(nil, "").ScopeSearch
		filter := &scopeFilter{grants: auth.GrantsOf, repoProjects: func(repo string) []scope {
			m, ok := h.srv.Workspace().Lookup(repo)
			if !ok {
				return nil
			}
			var out []scope
			for _, ref := range m.Vault.Projects() {
				out = append(out, scope{key: string(ref.Key), team: ref.Team})
			}
			return out
		}}
		rg := &routeGuard{
			routes: func() chi.Routes {
				routes, _ := h.srv.Handler().(chi.Routes)
				return routes
			},
			grants: auth.GrantsOf,
			filter: filter.response,
		}
		opts.Middleware = rg.middleware
		opts.EventFilter = filter.event
	}
	srv, err := server.New(opts) //nolint:contextcheck // server.New takes no context; it mounts synchronously and listens only in Start
	if err != nil {
		return nil, fmt.Errorf("build the server: %w", err)
	}
	h.srv = srv
	h.syncer.target = srv
	return h, nil
}

// commitPushDelay coalesces the pushes of a burst of commits into one sync.
const commitPushDelay = 2 * time.Second

// serverOptions maps the hub configuration onto the companion's server.
func serverOptions(cfg *Config, deps Deps, log *slog.Logger, onCommit func()) server.Options {
	repos := make([]server.Repo, 0, len(cfg.Repos()))
	for _, r := range cfg.Repos() {
		role := "project"
		if cfg.Organization.Team != nil && r == cfg.Organization.Team {
			role = "team"
		}
		repos = append(repos, server.Repo{ID: r.ID, Path: cfg.RepoDir(r.ID), Role: role})
	}
	return server.Options{
		Bind:      cfg.Bind,
		Port:      cfg.Port,
		Token:     cfg.Token,
		Version:   deps.Version,
		Commit:    deps.Commit,
		Mode:      Mode,
		UI:        deps.UI,
		Logger:    log,
		Repos:     repos,
		Workspace: cfg.Organization.ID,
		// Writes through the hub land on disk through the vault, which
		// reindexes them itself; the watcher is what notices the files a pull
		// brings in.
		Watch: true,
		Git: config.Git{
			Backend:        config.BackendSystem,
			CommitOnSave:   true,
			PullStrategy:   config.PullStrategy(gitops.StrategyRebase),
			PushOnSync:     true,
			MaxPushRetries: 3,
			AuthorName:     cfg.Git.AuthorName,
			AuthorEmail:    cfg.Git.AuthorEmail,
		},
		CacheDir:      cfg.CacheDir(),
		MCPHTTP:       true,
		MCPAllowWrite: true,
		MCPAgent:      "gintrack-hub",
		MCPHubTools:   true,
		OnCommit:      func(string, string) { onCommit() },
	}
}

// Server returns the underlying server, for tests and for the binary's banner.
func (h *Hub) Server() *server.Server { return h.srv }

// Run serves until ctx is cancelled, syncing with the forge in the background.
func (h *Hub) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.syncer.run(ctx)
	}()
	err := h.srv.Start(ctx)
	cancel()
	<-done
	if err != nil {
		return fmt.Errorf("serve: %w", err)
	}
	return nil
}

// syncTarget is the slice of *server.Server the loop needs.
type syncTarget interface {
	SyncNow(ctx context.Context) []gitops.SyncResult
}

// syncLoop syncs every repository on a timer, and shortly after a commit so
// that what a user saved reaches the forge without waiting for the timer.
type syncLoop struct {
	target   syncTarget
	interval time.Duration
	delay    time.Duration
	kick     chan struct{}
	log      *slog.Logger
}

func newSyncLoop(interval, delay time.Duration, log *slog.Logger) *syncLoop {
	return &syncLoop{interval: interval, delay: delay, kick: make(chan struct{}, 1), log: log}
}

// trigger asks for a sync soon. It never blocks: a pending request already
// covers this one.
func (l *syncLoop) trigger() {
	select {
	case l.kick <- struct{}{}:
	default:
	}
}

// run syncs once at start, then on every tick and after every trigger.
func (l *syncLoop) run(ctx context.Context) {
	l.once(ctx)
	ticker := time.NewTicker(l.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-l.kick:
			select {
			case <-ctx.Done():
				return
			case <-time.After(l.delay):
			}
			// Commits made during the delay are covered by this sync.
			select {
			case <-l.kick:
			default:
			}
		}
		l.once(ctx)
	}
}

func (l *syncLoop) once(ctx context.Context) {
	if l.target == nil {
		return
	}
	for _, res := range l.target.SyncNow(ctx) {
		if !res.OK() {
			l.log.Warn("sync with the forge failed", "repo", res.Repo, "code", res.Code, "message", res.Message)
		}
	}
}
