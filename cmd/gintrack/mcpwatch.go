package main

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	corevault "github.com/digiogithub/git-in-track/internal/vault"
	"github.com/digiogithub/git-in-track/internal/watcher"
)

// mcpRescanInterval bounds how often the fallback rescans a repository the
// watcher could not cover: an agent calls tools in bursts, and one pass per
// burst is enough to see what a human changed between two of them.
const mcpRescanInterval = time.Second

// mcpMount is one repository the stdio server serves.
type mcpMount struct {
	id   string
	root string
	role string
	docs []string
	vlt  *corevault.Vault
}

// mcpWatcher is the slice of internal/watcher the stdio server drives. It is an
// interface so that a test can make the watcher fail to start.
type mcpWatcher interface {
	AddRepoScoped(key, root string, scopes []string) error
	Events() <-chan []watcher.Event
	Errors() <-chan error
	Close() error
}

// mcpFreshness keeps the indexes of a long-lived `gintrack mcp` current with
// the files. The server indexes the workspace once at startup; without this a
// task triaged out of the inbox, or a page written, in the web UI or by another
// agent afterwards was invisible to every list and search until the agent
// runtime restarted the server.
//
// Every repository is watched the way the companion watches it. One the
// watcher cannot cover — the watcher failed to start, or the repository could
// not be added — falls back to an incremental rescan before a tool call, at
// most once per mcpRescanInterval.
type mcpFreshness struct {
	mounts     []mcpMount
	log        *slog.Logger
	newWatcher func(watcher.Options) (mcpWatcher, error)
	now        func() time.Time
	interval   time.Duration

	mu       sync.Mutex
	polled   []mcpMount
	lastScan time.Time
}

// newMCPFreshness builds the keeper over the mounted repositories. Nothing is
// watched until start: `gintrack mcp --list-tools` never needs a watcher.
func newMCPFreshness(mounts []mcpMount, log *slog.Logger) *mcpFreshness {
	return &mcpFreshness{
		mounts: mounts,
		log:    log,
		newWatcher: func(opts watcher.Options) (mcpWatcher, error) {
			w, err := watcher.New(opts)
			if err != nil {
				return nil, fmt.Errorf("start the file watcher: %w", err)
			}
			return w, nil
		},
		now:      time.Now,
		interval: mcpRescanInterval,
	}
}

// start watches every repository it can and folds each batch into the index of
// the repository it belongs to. The returned function stops the watcher and
// waits for its loop to drain.
func (f *mcpFreshness) start(ctx context.Context) func() {
	w, err := f.newWatcher(watcher.Options{Logger: f.log})
	if err != nil {
		f.log.Warn("the file watcher could not start: rescanning before tool calls instead", "error", err)
		f.fallBack(f.mounts...)
		return func() {}
	}

	byID := make(map[string]*corevault.Vault, len(f.mounts))
	for _, m := range f.mounts {
		if err := w.AddRepoScoped(m.id, m.root, m.vlt.WatchScopes(m.docs)); err != nil {
			f.log.Warn("a repository cannot be watched: rescanning it before tool calls instead",
				"repo", m.id, "error", err)
			f.fallBack(m)
			continue
		}
		byID[m.id] = m.vlt
	}
	if len(byID) == 0 {
		_ = w.Close()
		return func() {}
	}

	done := make(chan struct{})
	go f.watchLoop(ctx, w, byID, done)
	return func() {
		if err := w.Close(); err != nil {
			f.log.Debug("closing the watcher", "error", err)
		}
		<-done
	}
}

// fallBack hands repositories to the rescan fallback.
func (f *mcpFreshness) fallBack(mounts ...mcpMount) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.polled = append(f.polled, mounts...)
}

// watchLoop applies every batch until the watcher closes or ctx ends.
func (f *mcpFreshness) watchLoop(
	ctx context.Context, w mcpWatcher, byID map[string]*corevault.Vault, done chan struct{},
) {
	defer close(done)
	events := w.Events()
	errs := w.Errors()
	for {
		select {
		case <-ctx.Done():
			return
		case err, ok := <-errs:
			if !ok {
				errs = nil
				continue
			}
			f.log.Warn("file watcher", "error", err)
		case batch, ok := <-events:
			if !ok {
				return
			}
			for key, evs := range watcher.GroupByRepo(batch) {
				v, ok := byID[key]
				if !ok {
					continue
				}
				if _, err := v.ApplyEvents(ctx, watcher.ToFileEvents(evs)); err != nil {
					f.log.Warn("incremental index pass failed", "repo", key, "error", err)
				}
			}
		}
	}
}

// beforeCall rescans the repositories no watcher covers, unless the last pass
// is more recent than the interval. A failed pass is logged and the call goes
// on, answered from the index as it was.
func (f *mcpFreshness) beforeCall(ctx context.Context) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.polled) == 0 {
		return
	}
	now := f.now()
	if !f.lastScan.IsZero() && now.Sub(f.lastScan) < f.interval {
		return
	}
	f.lastScan = now
	for _, m := range f.polled {
		if _, err := m.vlt.Rescan(ctx); err != nil {
			f.log.Warn("rescan failed", "repo", m.id, "error", err)
		}
	}
}
