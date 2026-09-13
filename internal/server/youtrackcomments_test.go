package server

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/digiogithub/git-in-track/internal/core"
	"github.com/digiogithub/git-in-track/internal/syncengine"
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

// TestCommentPushWithoutAnEditorRefusesToDuplicate pins the honest failure of a
// build whose client cannot edit a remote comment: it says so rather than
// posting a second copy.
func TestCommentPushWithoutAnEditorRefusesToDuplicate(t *testing.T) {
	t.Parallel()

	root := copyTree(t, fixtureRoot)
	linkStoryToIssue(t, root, "DEMO-42")
	fake := newFakeYouTrack()
	s, _ := newJobServerIn(t, fake, root)

	if err := pushOne(t.Context(), t, s); err != nil {
		t.Fatalf("the first push failed: %v", err)
	}
	err := pushOne(t.Context(), t, s)
	if err == nil {
		t.Fatal("the second push succeeded without an edit endpoint")
	}
	if len(fake.created) != 1 {
		t.Errorf("the comment was posted twice: %v", fake.created)
	}
	if class := syncengine.Classify(err, jobClock).Class; class != syncengine.ClassTerminal {
		t.Errorf("class = %s, want terminal", class)
	}
}

// TestWriteCommentExternalRefusesAStaleFile is the optimistic lock of the
// write-back: a comment edited while it was in flight is reported, never
// overwritten.
func TestWriteCommentExternalRefusesAStaleFile(t *testing.T) {
	t.Parallel()

	root := copyTree(t, fixtureRoot)
	file := filepath.Join(root, filepath.FromSlash(pushedCommentPath))
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read the comment: %v", err)
	}
	rev := core.ComputeRev(data)

	ref := commentExternalRef("https://yt.example.com/youtrack", "DEMO-42", "4-9",
		core.NewTimestamp(jobClock))

	t.Run("the matching rev writes", func(t *testing.T) {
		if err := writeCommentExternal(file, pushedCommentPath, rev, ref); err != nil {
			t.Fatalf("writeCommentExternal(): %v", err)
		}
		if got := commentExternalID(t, root); got != "4-9" {
			t.Errorf("the comment records %q, want 4-9", got)
		}
	})

	t.Run("a stale rev is refused", func(t *testing.T) {
		err := writeCommentExternal(file, pushedCommentPath, rev, ref)
		if err == nil {
			t.Fatal("a stale write was accepted")
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
