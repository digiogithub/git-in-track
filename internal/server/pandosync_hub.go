package server

import (
	"context"
	"time"

	"github.com/digiogithub/git-in-track/internal/core"
	"github.com/digiogithub/git-in-track/internal/pandosync"
)

// corpusDebounce coalesces a burst of watcher events into one corpus write. The
// exporter's content hash already absorbs a repeated write, so this is about
// not walking the index a hundred times during a `git pull`, not about
// correctness (GIT-T-0134).
const corpusDebounce = 250 * time.Millisecond

// startCorpusSync brings the exported corpus up to date and keeps it there.
//
// The first full export runs in a goroutine started from Start, so the listener
// answers requests while a ten-thousand-item corpus is still being written, and
// it is cancelled by the server's shutdown context (GIT-T-0130).
func (s *Server) startCorpusSync(ctx context.Context) {
	if s.search == nil {
		return
	}
	s.search.armSync(ctx)
}

// armSync adopts the server's lifetime context and brings the corpus sync up.
func (s *searchState) armSync(ctx context.Context) {
	s.syncMu.Lock()
	s.syncCtx = ctx
	s.syncMu.Unlock()
	s.syncNow()
}

// syncNow starts whatever half of the corpus sync is not running yet. It is
// idempotent, and it is the path a settings change takes: a companion started
// without a corpus and given one through PATCH /search/settings has to start
// exporting and following the hub there and then — waiting for a restart is
// how a setting looks broken (GIT-US-0073).
//
// A full export runs every time exporters appear or are rebuilt, because the
// new ones know nothing of what is already on disk; it is cheap over an
// unchanged corpus, which the exporter skips by content hash. The hub follower
// is started at most once per process: it resolves the exporter of a
// repository per event, so it never holds a stale one.
//
// Both goroutines run under the server's lifetime context rather than the
// caller's — a corpus export must outlive the settings request that enabled
// it, and end with the server.
func (s *searchState) syncNow() {
	s.syncMu.Lock()
	run := s.syncCtx
	s.syncMu.Unlock()

	if run == nil || run.Err() != nil || len(s.allExporters()) == 0 {
		return
	}

	s.syncMu.Lock()
	follow := !s.following
	s.following = true
	s.syncMu.Unlock()

	go s.exportAll(run) //nolint:contextcheck // the server lifetime, deliberately not the caller's
	if follow {
		go s.followHub(run) //nolint:contextcheck // same: the follower outlives any one request
	}
}

// exportAll runs one full export of every repository's corpus.
func (s *searchState) exportAll(ctx context.Context) {
	for _, e := range s.allExporters() {
		if ctx.Err() != nil {
			return
		}
		start := s.now()
		stats, err := e.exp.ExportAll(ctx)
		switch {
		case ctx.Err() != nil:
			// A shutdown mid-export is not a failure: every file written is
			// complete, and the next start exports the rest.
			s.log.Debug("corpus export cancelled", "repo", e.repo)
			return
		case err != nil:
			s.log.Warn("corpus export failed", "repo", e.repo, "error", err)
			s.publishProgress("startup", e.repo, searchPhaseFailed, 0, 0, err.Error())
		default:
			s.log.Info("corpus exported", "repo", e.repo, "dir", e.exp.Dir(),
				"items", stats.Items, "pages", stats.Pages, "written", stats.Written,
				"removed", stats.Removed, "skipped", stats.Skipped,
				"duration", s.now().Sub(start))
			s.publishProgress("startup", e.repo, searchPhaseDone,
				stats.Items+stats.Pages, stats.Items+stats.Pages, "")
		}
	}
}

// followHub keeps the corpus in step with the index by subscribing to the same
// event stream the UIs follow.
//
// A subscriber the hub drops — one that could not keep up — cannot say which
// documents are stale any more, so it re-exports everything and subscribes
// again. That is the whole reason pandosync has an Overflow event: an
// incremental exporter that missed a change desyncs forever and never notices.
func (s *searchState) followHub(ctx context.Context) {
	for ctx.Err() == nil {
		client := newHubClient()
		client.subscribe([]string{eventItemChanged, eventFileChanged})
		s.hub.register(client)
		overflowed := s.drainHub(ctx, client)
		s.hub.unregister(client)
		if !overflowed {
			return
		}
		s.log.Warn("corpus sync fell behind the event stream; re-exporting the whole corpus")
		s.applyToAll(ctx, pandosync.Event{Kind: pandosync.Overflow})
	}
}

// drainHub consumes events until the context ends or the hub declares this
// subscriber too slow. It reports whether it stopped because of the latter.
func (s *searchState) drainHub(ctx context.Context, client *hubClient) bool {
	pending := map[string]corpusChange{}
	timer := time.NewTimer(corpusDebounce)
	if !timer.Stop() {
		<-timer.C
	}
	armed := false
	defer timer.Stop()

	flush := func() {
		armed = false
		for _, change := range sortedChanges(pending) {
			s.applyTo(ctx, change.repo, change.event)
		}
		clear(pending)
	}

	for {
		select {
		case <-ctx.Done():
			return false
		case <-client.overflowed:
			return true
		case ev := <-client.events:
			change, ok := corpusChangeOf(ev)
			if !ok {
				continue
			}
			pending[change.key()] = change
			if !armed {
				timer.Reset(corpusDebounce)
				armed = true
			}
		case <-timer.C:
			flush()
		}
	}
}

// corpusChange is one hub event translated into the exporter's vocabulary.
type corpusChange struct {
	repo  string
	event pandosync.Event
	// seq orders the flush, so a burst is replayed in the order it arrived.
	seq uint64
}

// key identifies the document a change is about, which is what collapses a
// burst over one file into a single write.
func (c corpusChange) key() string {
	return c.repo + "\x00" + string(c.event.Kind) + "\x00" + string(c.event.ID) + c.event.Path
}

// corpusChangeOf maps one hub event onto a pandosync event. It reports false
// for everything the corpus does not mirror: an event of another type, a file
// outside the knowledge base, a payload this process did not publish.
func corpusChangeOf(ev Event) (corpusChange, bool) {
	switch data := ev.Data.(type) {
	case itemChangedData:
		kind := pandosync.ItemChanged
		if data.Op == "deleted" {
			kind = pandosync.ItemRemoved
		}
		return corpusChange{
			repo:  data.Repo,
			event: pandosync.Event{Kind: kind, ID: core.ItemID(data.ID)},
			seq:   ev.Seq,
		}, data.ID != ""
	case fileChangedData:
		if !data.IsKb || data.Path == "" {
			return corpusChange{}, false
		}
		kind := pandosync.PageChanged
		if data.Op == "remove" || data.Op == "rename" {
			kind = pandosync.PageRemoved
		}
		return corpusChange{
			repo:  data.Repo,
			event: pandosync.Event{Kind: kind, Path: data.Path},
			seq:   ev.Seq,
		}, true
	default:
		return corpusChange{}, false
	}
}

// sortedChanges replays a debounced batch in arrival order.
func sortedChanges(pending map[string]corpusChange) []corpusChange {
	out := make([]corpusChange, 0, len(pending))
	for _, c := range pending {
		out = append(out, c)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].seq < out[j-1].seq; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// applyTo hands one event to the exporter of its repository. A write failure is
// logged and forgotten: the exporter is not poisoned by it, and the next event
// over the same document retries, so a transient full disk costs one document
// rather than the subscriber.
func (s *searchState) applyTo(ctx context.Context, repo string, ev pandosync.Event) {
	exp, ok := s.exporterFor(repo)
	if !ok {
		return
	}
	if _, err := exp.Apply(ctx, ev); err != nil && ctx.Err() == nil {
		s.log.Warn("corpus update failed", "repo", repo, "kind", ev.Kind,
			"id", ev.ID, "path", ev.Path, "error", err)
	}
}

// applyToAll hands one event to every exporter. It is how an overflow becomes a
// full re-export of every repository.
func (s *searchState) applyToAll(ctx context.Context, ev pandosync.Event) {
	for _, e := range s.allExporters() {
		if ctx.Err() != nil {
			return
		}
		if _, err := e.exp.Apply(ctx, ev); err != nil && ctx.Err() == nil {
			s.log.Warn("corpus re-export failed", "repo", e.repo, "error", err)
		}
	}
}
