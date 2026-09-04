package server

import (
	"context"
	"fmt"
	"sync"

	"github.com/digiogithub/git-in-track/internal/watcher"
)

// FileWatcher is the slice of internal/watcher this package uses. It is an
// interface so that a test can drive the server with a scripted stream of
// batches instead of real file-system events.
type FileWatcher interface {
	// AddRepo registers a repository tree under a key.
	AddRepo(key, root string) error
	// Events yields one batch per debounce window.
	Events() <-chan []watcher.Event
	// Errors yields non-fatal watcher problems.
	Errors() <-chan error
	// Close stops watching and closes the channels.
	Close() error
}

// WatcherFactory builds the watcher the server drives. Options.NewWatcher
// replaces it in tests.
type WatcherFactory func(watcher.Options) (FileWatcher, error)

// defaultWatcher builds the real fsnotify-backed watcher.
func defaultWatcher(opts watcher.Options) (FileWatcher, error) {
	w, err := watcher.New(opts)
	if err != nil {
		return nil, fmt.Errorf("start the file watcher: %w", err)
	}
	return w, nil
}

// watchState holds the running watcher, if there is one.
type watchState struct {
	mu      sync.Mutex
	watcher FileWatcher
	live    bool
	done    chan struct{}
}

// live reports whether the server is delivering file-system events. It is what
// /capabilities reports as the `watcher` feature.
func (s *Server) watching() bool {
	s.watch.mu.Lock()
	defer s.watch.mu.Unlock()
	return s.watch.live
}

// startWatch attaches a watcher to every mounted repository. A watcher that
// cannot be created, or that cannot watch anything, degrades the companion to
// "no live updates" with a warning: it never stops the server.
func (s *Server) startWatch(ctx context.Context) {
	if !s.opts.Watch {
		return
	}
	mounts := s.repos.ready()
	if len(mounts) == 0 {
		return
	}

	factory := s.opts.NewWatcher
	if factory == nil {
		factory = defaultWatcher
	}
	w, err := factory(watcher.Options{Debounce: s.opts.Debounce, Logger: s.log})
	if err != nil {
		s.log.Warn("live updates are off: the file watcher could not start", "error", err)
		return
	}

	watched := 0
	for _, m := range mounts {
		if err := w.AddRepo(m.id, m.path); err != nil {
			s.log.Warn("live updates are off for a repository", "repo", m.id, "error", err)
			continue
		}
		watched++
	}
	if watched == 0 {
		_ = w.Close()
		s.log.Warn("live updates are off: no repository could be watched")
		return
	}

	done := make(chan struct{})
	s.watch.mu.Lock()
	s.watch.watcher = w
	s.watch.live = true
	s.watch.done = done
	s.watch.mu.Unlock()

	go s.watchLoop(ctx, w, done)
}

// stopWatch closes the watcher and waits for its loop to drain.
func (s *Server) stopWatch() {
	s.watch.mu.Lock()
	w, done := s.watch.watcher, s.watch.done
	s.watch.watcher, s.watch.done, s.watch.live = nil, nil, false
	s.watch.mu.Unlock()

	if w == nil {
		return
	}
	if err := w.Close(); err != nil {
		s.log.Debug("closing the watcher", "error", err)
	}
	if done != nil {
		<-done
	}
}

// watchLoop folds every batch into the index and publishes what changed.
func (s *Server) watchLoop(ctx context.Context, w FileWatcher, done chan struct{}) {
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
			s.log.Warn("file watcher", "error", err)
		case batch, ok := <-events:
			if !ok {
				return
			}
			s.applyBatch(ctx, batch)
		}
	}
}

// applyBatch applies one debounced batch: it announces the raw file events,
// folds them into the index of the repository they belong to and announces the
// items the pass changed.
func (s *Server) applyBatch(ctx context.Context, batch []watcher.Event) {
	if len(batch) == 0 {
		return
	}
	for key, events := range watcher.GroupByRepo(batch) {
		m, ok := s.repos.lookup(key)
		if !ok || !m.ready() {
			continue
		}
		for _, ev := range events {
			s.hub.Publish(eventFileChanged, fileChangedData{
				Repo:    m.id,
				Path:    ev.Path,
				Op:      string(ev.Op),
				IsPmngr: isBacklogPath(ev.Path),
				IsKb:    !isBacklogPath(ev.Path),
			})
		}
		delta, err := m.vlt.ApplyEvents(ctx, watcher.ToFileEvents(events))
		if err != nil {
			s.log.Warn("incremental index pass failed", "repo", m.id, "error", err)
			continue
		}
		m.touch(s.now())
		s.publishDelta(m, delta)
	}
}
