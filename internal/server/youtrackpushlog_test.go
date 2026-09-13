package server

import (
	"context"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/digiogithub/git-in-track/internal/config"
	"github.com/digiogithub/git-in-track/internal/syncengine"
	"github.com/digiogithub/git-in-track/internal/vault"
)

// The automatic comment push as the server sees it: the setting reaching the
// vault, the logging cadence and the coalescing — tasks GIT-T-0164 and
// GIT-T-0173.

// TestPushCommentsReachesTheVaultLink covers the first criterion of
// GIT-T-0164: `integrations.youtrack.push_comments` is read from project.yaml,
// defaults to `manual`, and is carried into the link the vault reads back
// through the provider — which is the whole of the automatic push, because
// `Vault.autoPushComment` queues nothing unless that field says `auto`.
func TestPushCommentsReachesTheVaultLink(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		mode config.PushCommentsMode
		want string
	}{
		{name: "an unset mode defaults to manual", mode: "", want: vault.YouTrackPushManual},
		{name: "manual stays manual", mode: config.PushCommentsManual, want: vault.YouTrackPushManual},
		{name: "auto is carried across", mode: config.PushCommentsAuto, want: vault.YouTrackPushAuto},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			link := &config.YouTrackLink{
				URL: "https://yt.example.com/youtrack", Project: "ACME", PushComments: tc.mode,
			}
			s, _, _ := newYouTrackServer(t, link, ytToken, false)
			client, saved, err := s.youtrack.clientFor(ytProjectKey)
			if err != nil {
				t.Fatalf("resolve the client: %v", err)
			}
			if got := youtrackLinkOf(client, saved).PushComments; got != tc.want {
				t.Errorf("pushComments = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestPushDecisionIsLoggedOncePerItem covers the third criterion of
// GIT-T-0164: a thread of many comments announces itself once, not once per
// comment, and a second item gets its own line.
func TestPushDecisionIsLoggedOncePerItem(t *testing.T) {
	t.Parallel()

	state := &youtrackState{}
	for _, tc := range []struct {
		item string
		want bool
	}{
		{item: "DEMO-US-0001", want: true},
		{item: "DEMO-US-0001", want: false},
		{item: "DEMO-US-0001", want: false},
		{item: "DEMO-US-0002", want: true},
		{item: "DEMO-US-0002", want: false},
	} {
		if got := state.noteCommentPushItem(tc.item); got != tc.want {
			t.Errorf("noteCommentPushItem(%s) = %v, want %v", tc.item, got, tc.want)
		}
	}
}

// TestCommentPushItemIDReadsThePayload covers the payload reading the log line
// depends on, including the kinds and shapes that carry no item at all.
func TestCommentPushItemIDReadsThePayload(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		payload any
		want    string
	}{
		{name: "nil", payload: nil, want: ""},
		{name: "a comment job", payload: map[string]any{"itemId": "DEMO-US-0001"}, want: "DEMO-US-0001"},
		{name: "a payload with no item", payload: map[string]any{"path": "docs/index.md"}, want: ""},
		{name: "something that is not an object", payload: []string{"a"}, want: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := commentPushItemID(tc.payload); got != tc.want {
				t.Errorf("commentPushItemID() = %q, want %q", got, tc.want)
			}
		})
	}
}

// fakeEngineClock is a controllable syncengine.Clock, so the debounce window is
// observed by advancing time rather than by sleeping.
type fakeEngineClock struct {
	mu      sync.Mutex
	now     time.Time
	pending []*fakeEngineTimer
}

// fakeEngineTimer is one scheduled callback.
type fakeEngineTimer struct {
	clock *fakeEngineClock
	at    time.Time
	fn    func()
	done  bool
}

// Now reports the fake time.
func (c *fakeEngineClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// AfterFunc schedules a callback on the fake timeline.
func (c *fakeEngineClock) AfterFunc(d time.Duration, f func()) syncengine.Timer {
	c.mu.Lock()
	defer c.mu.Unlock()
	if d < 0 {
		d = 0
	}
	timer := &fakeEngineTimer{clock: c, at: c.now.Add(d), fn: f}
	c.pending = append(c.pending, timer)
	return timer
}

// Stop cancels a callback that has not fired.
func (t *fakeEngineTimer) Stop() bool {
	t.clock.mu.Lock()
	defer t.clock.mu.Unlock()
	if t.done {
		return false
	}
	t.done = true
	return true
}

// Advance moves the fake clock and runs everything that came due.
func (c *fakeEngineClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	var due []*fakeEngineTimer
	kept := c.pending[:0]
	for _, timer := range c.pending {
		switch {
		case timer.done:
		case !timer.at.After(c.now):
			timer.done = true
			due = append(due, timer)
		default:
			kept = append(kept, timer)
		}
	}
	c.pending = kept
	c.mu.Unlock()

	sort.SliceStable(due, func(i, j int) bool { return due[i].at.Before(due[j].at) })
	for _, timer := range due {
		timer.fn()
	}
}

// TestCommentPushIsCoalescedPerCommentPath covers GIT-T-0173: a burst of writes
// to one comment path inside the debounce window becomes one push job, and two
// different comment paths are never folded together.
//
// The window is the engine's own — its arm/AfterFunc/fire pair, on the clock
// injected here — so there is deliberately no second timer in this package to
// drift out of step with it. What the server adds is the idempotence key: the
// three path-keyed kinds enqueue under a stable id, so a burst is one job and
// the handler is not asked to push the same comment three times.
func TestCommentPushIsCoalescedPerCommentPath(t *testing.T) {
	t.Parallel()

	clock := &fakeEngineClock{now: jobClock}
	root := copyTree(t, fixtureRoot)
	s, err := New(Options{
		Token:      "test-token",
		Version:    "0.0.1-test",
		Workspace:  "test",
		Repos:      []Repo{{ID: testRepoID, Path: root, Role: "project", DocsFolder: "docs"}},
		Now:        func() time.Time { return jobClock },
		SyncEngine: SyncEngine{Debounce: time.Hour, Rate: -1, Workers: 1, Clock: clock},
	})
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	s.startSyncEngine(t.Context())
	t.Cleanup(func() { s.stopSyncEngine(context.WithoutCancel(t.Context())) })

	const otherPath = "docs/.pmngr/comments/DEMO-US-0001/20260901T104513Z-jose.md"
	enqueue := func(path string) string {
		t.Helper()
		id, err := s.EnqueueYouTrackCommentPush(t.Context(), YouTrackCommentPushRequest{
			Repo: testRepoID, Project: "DEMO",
			Params: YouTrackCommentPushParams{ItemID: "DEMO-US-0001", CommentPath: path},
		})
		if err != nil {
			t.Fatalf("enqueue %s: %v", path, err)
		}
		return id
	}

	first, second, third := enqueue(pushedCommentPath), enqueue(pushedCommentPath), enqueue(pushedCommentPath)
	other := enqueue(otherPath)

	if first != second || second != third {
		t.Errorf("three writes to one comment produced %s, %s and %s, want one job", first, second, third)
	}
	if other == first {
		t.Error("two different comment paths were folded into one job")
	}
	if got := s.sync.engine.Pending().Queued; got != 2 {
		t.Errorf("%d jobs are queued, want 2: one per comment path", got)
	}

	// The clock never advanced, so nothing fired: what was counted is the
	// queue, not a race with a worker.
	if got := len(s.sync.engine.DeadLetter()); got != 0 {
		t.Errorf("%d jobs went to the dead-letter list", got)
	}
}

// TestCoalescedJobIDIsLimitedToPathKeyedKinds pins which kinds fold: the three
// whose key is a path, and never the import, whose key is a query somebody
// asked for twice on purpose.
func TestCoalescedJobIDIsLimitedToPathKeyedKinds(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		kind syncengine.Kind
		key  string
		want bool
	}{
		{name: "a comment push folds", kind: kindYouTrackCommentPush, key: "docs/c.md", want: true},
		{name: "a kb publish folds", kind: kindYouTrackKBPublish, key: "docs/index.md", want: true},
		{name: "a kb pull folds", kind: kindYouTrackKBPull, key: "docs/index.md", want: true},
		{name: "an import does not", kind: kindYouTrackImport, key: "project: DEMO", want: false},
		{name: "neither does a keyless job", kind: kindYouTrackCommentPush, key: "  ", want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := coalescedJobID(tc.kind, testRepoID, tc.key)
			if (got != "") != tc.want {
				t.Errorf("coalescedJobID() = %q, want folded = %v", got, tc.want)
			}
		})
	}
	if coalescedJobID(kindYouTrackCommentPush, "a", "p") == coalescedJobID(kindYouTrackCommentPush, "b", "p") {
		t.Error("two repositories share one job id, so one would swallow the other")
	}
}
