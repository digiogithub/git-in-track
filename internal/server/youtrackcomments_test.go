package server

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/digiogithub/git-in-track/internal/core"
	"github.com/digiogithub/git-in-track/internal/syncengine"
	"github.com/digiogithub/git-in-track/internal/vault"
)

// The comment push, story GIT-US-0068.

// pushedCommentPath is the fixture comment every test here pushes.
const pushedCommentPath = "docs/.pmngr/comments/DEMO-US-0001/20260901T104512Z-marta.md"

// commentStoryPath is the item that comment belongs to.
const commentStoryPath = "docs/.pmngr/stories/DEMO-US-0001-guest-checkout.md"

// linkStoryToIssue writes a YouTrack `external` entry onto the fixture story,
// which is the precondition of every push: a comment goes to the issue its item
// mirrors, and an item that mirrors nothing has nowhere to send it.
func linkStoryToIssue(t *testing.T, root, issue string) {
	t.Helper()

	file := filepath.Join(root, filepath.FromSlash(commentStoryPath))
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read the story: %v", err)
	}
	block := "external:\n  - system: youtrack\n    id: " + issue +
		"\n    url: https://yt.example.com/youtrack/issue/" + issue + "\n"
	// Inserted just before the closing delimiter of the front matter.
	text := string(data)
	end := strings.Index(text[4:], "---\n") + 4
	if end < 4 {
		t.Fatal("the fixture story has no front matter")
	}
	if err := os.WriteFile(file, []byte(text[:end]+block+text[end:]), 0o600); err != nil {
		t.Fatalf("write the story: %v", err)
	}
}

// pushOne runs one comment push against the fixture.
func pushOne(ctx context.Context, t *testing.T, s *Server) error {
	t.Helper()

	return runJob(ctx, t, s.handleYouTrackCommentJobs, kindYouTrackCommentPush,
		youtrackJobPayload{Repo: testRepoID, Project: "DEMO"},
		YouTrackCommentPushParams{ItemID: "DEMO-US-0001", CommentPath: pushedCommentPath})
}

// commentExternalID reads the YouTrack id recorded on the fixture comment.
func commentExternalID(t *testing.T, root string) string {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(pushedCommentPath)))
	if err != nil {
		t.Fatalf("read the comment: %v", err)
	}
	comment, err := core.ParseComment(pushedCommentPath, data)
	if err != nil {
		t.Fatalf("parse the comment: %v", err)
	}
	ref, ok := youtrackExternalOf(comment.External)
	if !ok {
		return ""
	}
	return ref.ID
}

// TestCommentPushCreatesThenEdits is the idempotence contract in one test: the
// first push creates the remote comment and records its id, and the second —
// which is what a retry, a journal replay and a dead-letter retry all produce —
// edits that comment instead of posting a second one.
func TestCommentPushCreatesThenEdits(t *testing.T) {
	t.Parallel()

	root := copyTree(t, fixtureRoot)
	linkStoryToIssue(t, root, "DEMO-42")
	fake := &fakeEditableYouTrack{fakeYouTrack: newFakeYouTrack()}
	s, _ := newJobServerIn(t, fake, root)

	if err := pushOne(t.Context(), t, s); err != nil {
		t.Fatalf("the first push failed: %v", err)
	}
	if len(fake.created) != 1 {
		t.Fatalf("the first push created %d comments, want 1", len(fake.created))
	}
	if !strings.HasPrefix(fake.created[0], "DEMO-42|") {
		t.Errorf("the comment went to %q, want the issue DEMO-42", fake.created[0])
	}
	if got := commentExternalID(t, root); got != "4-1" {
		t.Fatalf("the comment records %q, want the created id 4-1", got)
	}

	if err := pushOne(t.Context(), t, s); err != nil {
		t.Fatalf("the second push failed: %v", err)
	}
	if len(fake.created) != 1 {
		t.Errorf("the second push posted again: %v", fake.created)
	}
	if len(fake.edited) != 1 {
		t.Fatalf("the second push made %d edits, want 1", len(fake.edited))
	}
	if !strings.HasPrefix(fake.edited[0], "DEMO-42|4-1|") {
		t.Errorf("the edit addressed %q, want DEMO-42|4-1", fake.edited[0])
	}
}

// TestCommentPushCarriesTheAttributionLine proves the posted text is the
// comment plus one trailing line naming the author and the item.
func TestCommentPushCarriesTheAttributionLine(t *testing.T) {
	t.Parallel()

	root := copyTree(t, fixtureRoot)
	linkStoryToIssue(t, root, "DEMO-42")
	fake := newFakeYouTrack()
	s, _ := newJobServerIn(t, fake, root)

	if err := pushOne(t.Context(), t, s); err != nil {
		t.Fatalf("the push failed: %v", err)
	}
	text := strings.SplitN(fake.created[0], "|", 2)[1]
	if !strings.Contains(text, "The provider rejects addresses") {
		t.Error("the posted text does not carry the comment body")
	}
	if !strings.Contains(text, "marta") || !strings.Contains(text, "DEMO-US-0001") {
		t.Errorf("the attribution line names neither the author nor the item:\n%s", text)
	}
	if strings.Contains(text, "[DEMO-US-0001]") || strings.Contains(text, "(DEMO-US-0001") {
		t.Errorf("the item id was wrapped in a link, which YouTrack would not resolve:\n%s", text)
	}
}

// TestRenderCommentText covers the template renderer on its own, including the
// failure a broken project.yaml produces.
func TestRenderCommentText(t *testing.T) {
	t.Parallel()

	comment := &core.Comment{
		Item: "DEMO-US-0001", Author: "marta", AuthorName: "Marta Ruiz",
		Body: "A decision.\n", Path: "docs/.pmngr/comments/DEMO-US-0001/x.md",
	}

	tests := []struct {
		name     string
		template string
		want     string
		wantErr  bool
	}{
		{
			name:     "the shipped default names the author and the item",
			template: defaultCommentAttribution,
			want:     "A decision.\n\n---\n_marta · git-in-track DEMO-US-0001_",
		},
		{
			name:     "a project template wins",
			template: "\n\nfrom {{.AuthorName}} about {{.IssueID}}",
			want:     "A decision.\n\nfrom Marta Ruiz about DEMO-42",
		},
		{
			name:     "an empty template posts the body alone",
			template: "",
			want:     "A decision.",
		},
		{name: "a template that does not parse fails the job", template: "{{", wantErr: true},
		{name: "a template naming nothing real fails the job", template: "{{.Nope}}", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := renderCommentText(comment, tc.template, "DEMO-42")
			if tc.wantErr {
				if err == nil {
					t.Fatal("a broken template was accepted")
				}
				if class := syncengine.Classify(err, jobClock).Class; class != syncengine.ClassTerminal {
					t.Errorf("class = %s, want terminal: a broken template cannot be retried into working", class)
				}
				return
			}
			if err != nil {
				t.Fatalf("renderCommentText(): %v", err)
			}
			if got != tc.want {
				t.Errorf("text = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestCommentPushFailsTerminallyWithoutAnItemLink is the acceptance criterion
// of GIT-US-0068: an item that mirrors no issue fails the job clearly and
// without retrying, because no number of attempts creates a link only a user
// can create.
func TestCommentPushFailsTerminallyWithoutAnItemLink(t *testing.T) {
	t.Parallel()

	fake := newFakeYouTrack()
	s, _ := newJobServer(t, fake)

	err := pushOne(t.Context(), t, s)
	if err == nil {
		t.Fatal("a push against an unlinked item succeeded")
	}
	if class := syncengine.Classify(err, jobClock).Class; class != syncengine.ClassTerminal {
		t.Errorf("class = %s, want terminal", class)
	}
	if !strings.Contains(err.Error(), "DEMO-US-0001") {
		t.Errorf("the message does not name the item: %v", err)
	}
	if len(fake.created) != 0 {
		t.Error("a comment was posted anyway")
	}
}

// TestCommentPushEditsRatherThanDuplicates is the idempotence of the handler:
// a job delivered twice — after a retry, a journal replay or a dead-letter
// retry — edits the remote comment it already created instead of posting a
// second copy.
func TestCommentPushEditsRatherThanDuplicates(t *testing.T) {
	t.Parallel()

	root := copyTree(t, fixtureRoot)
	linkStoryToIssue(t, root, "DEMO-42")
	fake := newFakeYouTrack()
	s, _ := newJobServerIn(t, fake, root)

	if err := pushOne(t.Context(), t, s); err != nil {
		t.Fatalf("the first push failed: %v", err)
	}
	if err := pushOne(t.Context(), t, s); err != nil {
		t.Fatalf("the second push failed: %v", err)
	}
	if len(fake.created) != 1 {
		t.Errorf("the comment was posted twice: %v", fake.created)
	}
	if len(fake.edited) != 1 {
		t.Fatalf("the re-delivered job edited %d times, want once: %v", len(fake.edited), fake.edited)
	}
	if !strings.Contains(fake.edited[0], "DEMO-42|") {
		t.Errorf("the edit did not name the issue and the comment: %q", fake.edited[0])
	}
}

// TestRecordCommentExternalRefusesAStaleFile is the optimistic lock of the
// write-back: a comment edited while it was in flight is reported, never
// overwritten. The lock is the vault's own, taken by "comment.update"; this
// proves the job still classifies the refusal as retryable, so the next
// attempt pushes what is on disk now.
func TestRecordCommentExternalRefusesAStaleFile(t *testing.T) {
	t.Parallel()

	root := copyTree(t, fixtureRoot)
	s, _ := newJobServerIn(t, newFakeYouTrack(), root)
	m, ok := s.repos.lookup(testRepoID)
	if !ok {
		t.Fatal("the fixture repository is not mounted")
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(pushedCommentPath)))
	if err != nil {
		t.Fatalf("read the comment: %v", err)
	}
	rev := core.ComputeRev(data)
	params := YouTrackCommentPushParams{ItemID: "DEMO-US-0001", CommentPath: pushedCommentPath}
	ref := commentExternalRef("https://yt.example.com/youtrack", "DEMO-42", "4-9",
		core.NewTimestamp(jobClock))

	t.Run("the matching rev writes", func(t *testing.T) {
		writes, err := s.recordCommentExternal(t.Context(), m, params, rev, ref)
		if err != nil {
			t.Fatalf("recordCommentExternal(): %v", err)
		}
		if len(writes.Written) != 1 || writes.Written[0].Path != pushedCommentPath {
			t.Errorf("write set = %+v, want the one comment file", writes)
		}
		if got := commentExternalID(t, root); got != "4-9" {
			t.Errorf("the comment records %q, want 4-9", got)
		}
	})

	t.Run("a stale rev is refused", func(t *testing.T) {
		_, err := s.recordCommentExternal(t.Context(), m, params, rev, ref)
		if err == nil {
			t.Fatal("a stale write was accepted")
		}
		if !errors.Is(err, errCommentStale) {
			t.Errorf("err = %v, want the stale-comment failure", err)
		}
		if class := syncengine.Classify(err, jobClock).Class; class != syncengine.ClassRetryable {
			t.Errorf("class = %s, want retryable: the next attempt pushes what is there now", class)
		}
	})
}

// TestUpsertExternalReplacesOneSystem proves the write-back updates the
// YouTrack entry and keeps every other system, which is what makes a page or a
// comment that mirrors two trackers survive a push to one of them.
func TestUpsertExternalReplacesOneSystem(t *testing.T) {
	t.Parallel()

	list := []core.External{
		{System: "jira", ID: "PROJ-1"},
		{System: "youtrack", ID: "4-1"},
		{System: "plane", ID: "p-9"},
	}
	got := upsertExternal(list, core.External{System: "youtrack", ID: "4-2"})
	if len(got) != 3 {
		t.Fatalf("the list grew to %d entries: %+v", len(got), got)
	}
	if got[1].ID != "4-2" {
		t.Errorf("the YouTrack entry was not replaced in place: %+v", got)
	}
	if got[0].System != "jira" || got[2].System != "plane" {
		t.Errorf("another system was disturbed: %+v", got)
	}

	added := upsertExternal([]core.External{{System: "jira", ID: "PROJ-1"}},
		core.External{System: "youtrack", ID: "4-3"})
	if len(added) != 2 || added[1].ID != "4-3" {
		t.Errorf("a first reference was not appended: %+v", added)
	}
}

// ------------------------------------------------------------ the route ---

// The HTTP surface of the comment push, story GIT-US-0076.

// newCommentPushAPIServer builds a companion over a linked copy of the fixture
// whose story already mirrors an issue, with the engine running so a push can
// actually queue.
func newCommentPushAPIServer(t *testing.T) (*Server, string) {
	t.Helper()

	root := copyTree(t, fixtureRoot)
	linkFixtureToYouTrack(t, root)
	linkStoryToIssue(t, root, "DEMO-42")
	s, _ := newJobServerIn(t, newFakeYouTrack(), root)
	s.youtrack.mu.Lock()
	s.youtrack.tokens.Set("DEMO", "perm:test-token")
	s.youtrack.mu.Unlock()
	s.startSyncEngine(t.Context())
	t.Cleanup(func() { s.stopSyncEngine(context.WithoutCancel(t.Context())) })
	return s, root
}

// TestCommentPushRouteQueues covers the happy path: the route answers 202 with
// the job id and the three per-comment lists the vault produced.
func TestCommentPushRouteQueues(t *testing.T) {
	t.Parallel()

	s, _ := newCommentPushAPIServer(t)
	var got vault.YouTrackCommentPushResult
	decode(t, send(t, s, request{
		method: http.MethodPost,
		target: "/api/v1/youtrack/comments/push?key=DEMO",
		body: map[string]any{
			"itemId":      "DEMO-US-0001",
			"commentPath": pushedCommentPath,
		},
	}), http.StatusAccepted, &got)

	if got.JobID == "" {
		t.Error("the answer carries no job id")
	}
	if got.ItemID != "DEMO-US-0001" || got.Project != "DEMO" {
		t.Errorf("answer = %+v", got)
	}
	if len(got.Pushed) != 1 || got.Pushed[0].CommentPath != pushedCommentPath {
		t.Errorf("pushed = %+v, want the one comment", got.Pushed)
	}
	if len(got.Failed) != 0 {
		t.Errorf("failed = %+v", got.Failed)
	}
}

// newCoalescingPushServer is newCommentPushAPIServer with a real coalescing
// window instead of the immediate dispatch every other case here wants.
//
// The window is what makes coalescing observable at all. With `Debounce: -1` a
// job runs the moment it is enqueued, so the first push can finish — writing
// the comment's `external` entry — before the second request arrives, and the
// second is then legitimately *skipped* as already delivered rather than folded
// into the first. Both outcomes are correct in production; only one of them is
// the thing under test, so the window holds both pushes in one batch.
func newCoalescingPushServer(t *testing.T) (*Server, string) {
	t.Helper()

	root := copyTree(t, fixtureRoot)
	linkFixtureToYouTrack(t, root)
	linkStoryToIssue(t, root, "DEMO-42")
	s, err := New(Options{
		Token:     "test-token",
		Version:   "0.0.1-test",
		Workspace: "test",
		Repos:     []Repo{{ID: testRepoID, Path: root, Role: "project", DocsFolder: "docs"}},
		Now:       func() time.Time { return jobClock },
		// Long enough that no job can run between two requests of one test, and
		// never waited on: the test observes the queue, not the work.
		SyncEngine: SyncEngine{Debounce: time.Minute},
	})
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	s.youtrack.mu.Lock()
	s.youtrack.jobClient = func(string) (youtrackJobClient, vault.YouTrackLink, error) {
		return newFakeYouTrack(), vault.YouTrackLink{
			BaseURL: "https://yt.example.com/youtrack", Project: "DEMO",
		}, nil
	}
	s.youtrack.tokens.Set("DEMO", "perm:test-token")
	s.youtrack.mu.Unlock()
	s.startSyncEngine(t.Context())
	t.Cleanup(func() { s.stopSyncEngine(context.WithoutCancel(t.Context())) })
	return s, root
}

// TestCommentPushRouteCoalescesOnTheCommentPath pins the coalescing key: two
// pushes of one comment are one job, and a second comment of the same item is
// its own — the key is the comment path, not the item id.
func TestCommentPushRouteCoalescesOnTheCommentPath(t *testing.T) {
	t.Parallel()

	s, root := newCoalescingPushServer(t)
	other := "docs/.pmngr/comments/DEMO-US-0001/20260902T091200Z-jose.md"
	writeSecondComment(t, root, other)
	m, ok := s.repos.lookup(testRepoID)
	if !ok {
		t.Fatal("the fixture repository is not mounted")
	}
	// The comment was written behind the vault's back, so the index has to be
	// folded forward before the push can select it.
	if _, err := m.reindex(t.Context(), s.now); err != nil {
		t.Fatalf("reindex: %v", err)
	}

	push := func(path string) string {
		t.Helper()
		var got vault.YouTrackCommentPushResult
		decode(t, send(t, s, request{
			method: http.MethodPost,
			target: "/api/v1/youtrack/comments/push?key=DEMO",
			body:   map[string]any{"itemId": "DEMO-US-0001", "commentPath": path},
		}), http.StatusAccepted, &got)
		return got.JobID
	}
	first, second := push(pushedCommentPath), push(pushedCommentPath)
	third := push(other)
	if first == "" || first != second {
		t.Errorf("two pushes of one comment produced %q and %q, want one job", first, second)
	}
	if third == first {
		t.Error("two comments of one item were folded into one job: the key is the item id, not the path")
	}
}

// writeSecondComment adds another comment to the fixture story's thread.
func writeSecondComment(t *testing.T, root, rel string) {
	t.Helper()

	body := "---\nitem: DEMO-US-0001\nauthor: jose\ncreated: 2026-09-02T09:12:00Z\ntype: comment\n---\n\nSecond.\n"
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create the comment folder: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil { //nolint:gosec // a fixture copy
		t.Fatalf("write the comment: %v", err)
	}
}

// TestCommentPushRouteRefusals covers what the route refuses, and that it
// refuses before anything is queued: an unknown project, an unlinked item and a
// request naming neither a comment nor the whole thread.
func TestCommentPushRouteRefusals(t *testing.T) {
	t.Parallel()

	t.Run("an unlinked item", func(t *testing.T) {
		t.Parallel()

		root := copyTree(t, fixtureRoot)
		linkFixtureToYouTrack(t, root)
		s, _ := newJobServerIn(t, newFakeYouTrack(), root)
		s.youtrack.mu.Lock()
		s.youtrack.tokens.Set("DEMO", "perm:test-token")
		s.youtrack.mu.Unlock()
		s.startSyncEngine(t.Context())
		t.Cleanup(func() { s.stopSyncEngine(context.WithoutCancel(t.Context())) })

		rec := send(t, s, request{
			method: http.MethodPost,
			target: "/api/v1/youtrack/comments/push?key=DEMO",
			body:   map[string]any{"itemId": "DEMO-US-0001", "all": true},
		})
		if rec.Code == http.StatusAccepted {
			t.Fatalf("an item mirroring no issue was queued: %s", rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "import or link") {
			t.Errorf("the problem does not say what to do: %s", rec.Body.String())
		}
	})

	t.Run("neither a comment nor the whole thread", func(t *testing.T) {
		t.Parallel()

		s, _ := newCommentPushAPIServer(t)
		rec := send(t, s, request{
			method: http.MethodPost,
			target: "/api/v1/youtrack/comments/push?key=DEMO",
			body:   map[string]any{"itemId": "DEMO-US-0001"},
		})
		if rec.Code == http.StatusAccepted {
			t.Fatalf("a request selecting nothing was queued: %s", rec.Body.String())
		}
	})

	t.Run("a project with no connection", func(t *testing.T) {
		t.Parallel()

		s, _ := newJobServerIn(t, newFakeYouTrack(), copyTree(t, fixtureRoot))
		rec := send(t, s, request{
			method: http.MethodPost,
			target: "/api/v1/youtrack/comments/push?key=DEMO",
			body:   map[string]any{"itemId": "DEMO-US-0001", "all": true},
		})
		if rec.Code != http.StatusConflict {
			t.Fatalf("status = %d, want 409 youtrack_not_configured: %s", rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), codeYouTrackNotConfigured) {
			t.Errorf("the problem code is wrong: %s", rec.Body.String())
		}
	})

	t.Run("without the bearer token", func(t *testing.T) {
		t.Parallel()

		s, _ := newCommentPushAPIServer(t)
		res := do(t, s, http.MethodPost, "/api/v1/youtrack/comments/push?key=DEMO", nil)
		defer func() { _ = res.Body.Close() }()
		if res.StatusCode != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", res.StatusCode)
		}
	})
}
