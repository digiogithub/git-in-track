package mcp

import (
	"context"
	"fmt"
	"testing"

	"github.com/digiogithub/git-in-track/internal/vault"
	"github.com/digiogithub/git-in-track/internal/youtrack"
)

// The YouTrack tools, driven through the in-memory client session like every
// other tool test: what is asserted is what a client actually receives.
//
// The fake below stands in for the REST client the companion installs, so the
// whole surface runs with no network and no credentials.

// fakeTracker answers the vault's YouTrackSource contract from fixed data.
type fakeTracker struct {
	issues   map[string]youtrack.Issue
	articles map[string]youtrack.Article
}

func (f *fakeTracker) Issue(_ context.Context, id string) (youtrack.Issue, error) {
	issue, ok := f.issues[id]
	if !ok {
		return youtrack.Issue{}, fmt.Errorf("no issue %s", id)
	}
	return issue, nil
}

func (f *fakeTracker) Article(_ context.Context, id string) (youtrack.Article, error) {
	article, ok := f.articles[id]
	if !ok {
		return youtrack.Article{}, fmt.Errorf("no article %s", id)
	}
	return article, nil
}

func (f *fakeTracker) IssueLinks(_ context.Context, id string) ([]youtrack.IssueLink, error) {
	return f.issues[id].Links, nil
}

func (f *fakeTracker) SearchAllIssues(
	_ context.Context, _ string, _ youtrack.Page,
) ([]youtrack.Issue, error) {
	return nil, nil
}

func (f *fakeTracker) AllComments(_ context.Context, _ string) ([]youtrack.Comment, error) {
	return nil, nil
}

func (f *fakeTracker) Attachments(_ context.Context, _ string) ([]youtrack.Attachment, error) {
	return nil, nil
}

// queuedJobs records what the vault handed to the host instead of running it.
type queuedJobs struct {
	jobs []vault.YouTrackJob
}

func (q *queuedJobs) enqueue(_ context.Context, job vault.YouTrackJob) (string, error) {
	q.jobs = append(q.jobs, job)
	return fmt.Sprintf("job-%d", len(q.jobs)), nil
}

// newYouTrackHarness is the writable harness with a fake tracker, a recording
// queue and a project link in the given comment-push mode.
func newYouTrackHarness(t *testing.T, mode string) (*harness, *queuedJobs) {
	t.Helper()
	h := newHarness(t, true)
	queue := &queuedJobs{}
	mount, ok := h.space.MountForProject("DEMO")
	if !ok {
		t.Fatal("the fixture workspace serves no DEMO project")
	}
	tracker := &fakeTracker{
		issues: map[string]youtrack.Issue{
			"ACME-1": {ID: "2-1", IDReadable: "ACME-1", Summary: "A linked story"},
		},
		articles: map[string]youtrack.Article{},
	}
	mount.Vault.SetYouTrackProvider(
		func(context.Context, string) (vault.YouTrackSource, vault.YouTrackLink, error) {
			return tracker, vault.YouTrackLink{
				BaseURL: "https://yt.example.com", Project: "ACME", PushComments: mode,
			}, nil
		})
	mount.Vault.SetYouTrackEnqueuer(queue.enqueue)
	return h, queue
}

func TestImportYouTrackIssuesValidation(t *testing.T) {
	h, _ := newYouTrackHarness(t, vault.YouTrackPushManual)

	cases := []struct {
		name string
		args map[string]any
	}{
		{name: "neither a query nor ids", args: map[string]any{"project": "DEMO"}},
		{
			name: "both a query and ids",
			args: map[string]any{
				"project": "DEMO", "query": "State: Open", "ids": []string{"ACME-1"},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := callFails(t, h, "import_youtrack_issues", tc.args)
			if got.Code != codeInvalidRequest {
				t.Errorf("code = %q, want %q", got.Code, codeInvalidRequest)
			}
			if got.Field != "query" {
				t.Errorf("field = %q, want the offending argument named", got.Field)
			}
		})
	}
}

func TestImportYouTrackIssuesDryRunWritesNothing(t *testing.T) {
	h, _ := newYouTrackHarness(t, vault.YouTrackPushManual)

	got := call[ImportResult](t, h, "import_youtrack_issues", map[string]any{
		"project": "DEMO", "ids": []string{"ACME-1"}, "dryRun": true,
	})
	if !got.DryRun {
		t.Error("the result does not report itself as a dry run")
	}
	if len(got.Issues) != 1 || got.Issues[0].YouTrackID != "ACME-1" {
		t.Fatalf("issues = %+v", got.Issues)
	}
	if len(got.Changed) != 0 {
		t.Errorf("changed = %v: a dry run writes nothing", got.Changed)
	}
	if len(h.writes) != 0 {
		t.Errorf("announced %d writes for a dry run", len(h.writes))
	}
}

func TestImportYouTrackIssuesWritesAndAnnounces(t *testing.T) {
	h, _ := newYouTrackHarness(t, vault.YouTrackPushManual)

	got := call[ImportResult](t, h, "import_youtrack_issues", map[string]any{
		"project": "DEMO", "ids": []string{"ACME-1"},
	})
	if got.Created != 1 || got.Failed != 0 {
		t.Fatalf("created = %d, failed = %d: %+v", got.Created, got.Failed, got.Issues)
	}
	if got.Issues[0].ItemID == "" || got.Issues[0].Rev == "" {
		t.Errorf("the imported issue carries no item id and rev: %+v", got.Issues[0])
	}
	if len(got.Changed) == 0 {
		t.Error("the result lists no changed file")
	}
	if len(h.writes) != 1 || h.writes[0].Tool != "import_youtrack_issues" {
		t.Errorf("write events = %+v, want one from the import", h.writes)
	}
}

// TestAddCommentReachesTheSharedPushSeam is the point of GIT-US-0072: an
// agent-written comment queues a push through the vault's own "comment.add",
// with no push decision implemented in this package.
func TestAddCommentReachesTheSharedPushSeam(t *testing.T) {
	for _, tc := range []struct {
		name string
		mode string
		want int
	}{
		{name: "auto queues exactly one push", mode: vault.YouTrackPushAuto, want: 1},
		{name: "manual queues nothing", mode: vault.YouTrackPushManual, want: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, queue := newYouTrackHarness(t, tc.mode)
			imported := call[ImportResult](t, h, "import_youtrack_issues", map[string]any{
				"project": "DEMO", "ids": []string{"ACME-1"},
			})
			item := imported.Issues[0].ItemID
			queue.jobs = nil

			rev := call[ItemResult](t, h, "get_item", map[string]any{"id": item}).Item.Rev
			call[CommentResult](t, h, "add_comment", map[string]any{
				"id": item, "body": "Progress report.", "rev": rev,
			})

			if len(queue.jobs) != tc.want {
				t.Fatalf("queued %d jobs, want %d: %+v", len(queue.jobs), tc.want, queue.jobs)
			}
			if tc.want == 1 && queue.jobs[0].Kind != vault.JobKindCommentPush {
				t.Errorf("kind = %q, want %q", queue.jobs[0].Kind, vault.JobKindCommentPush)
			}
		})
	}
}

func TestPushCommentToYouTrack(t *testing.T) {
	t.Run("all queues every comment of a linked item", func(t *testing.T) {
		h, queue := newYouTrackHarness(t, vault.YouTrackPushManual)
		imported := call[ImportResult](t, h, "import_youtrack_issues", map[string]any{
			"project": "DEMO", "ids": []string{"ACME-1"},
		})
		item := imported.Issues[0].ItemID
		rev := call[ItemResult](t, h, "get_item", map[string]any{"id": item}).Item.Rev
		call[CommentResult](t, h, "add_comment", map[string]any{
			"id": item, "body": "Progress report.", "rev": rev,
		})
		queue.jobs = nil

		got := call[PushCommentResult](t, h, "push_comment_to_youtrack", map[string]any{
			"project": "DEMO", "itemId": item, "all": true,
		})
		if len(got.Pushed) != 1 {
			t.Fatalf("pushed = %+v, want the one comment", got.Pushed)
		}
		if got.JobID == "" {
			t.Error("the result carries no job id")
		}
		if len(queue.jobs) != 1 || queue.jobs[0].Kind != vault.JobKindCommentPush {
			t.Errorf("queued %+v", queue.jobs)
		}
	})

	t.Run("an item with no reference is refused", func(t *testing.T) {
		h, _ := newYouTrackHarness(t, vault.YouTrackPushManual)
		got := callFails(t, h, "push_comment_to_youtrack", map[string]any{
			"project": "DEMO", "itemId": "DEMO-US-0001", "all": true,
		})
		if got.Code != codeInvalidRequest {
			t.Errorf("code = %q, want %q", got.Code, codeInvalidRequest)
		}
	})

	t.Run("one selection mode or the other", func(t *testing.T) {
		h, _ := newYouTrackHarness(t, vault.YouTrackPushManual)
		got := callFails(t, h, "push_comment_to_youtrack", map[string]any{
			"project": "DEMO", "itemId": "DEMO-US-0001",
		})
		if got.Field != "commentPath" {
			t.Errorf("field = %q, want commentPath", got.Field)
		}
	})
}

func TestKBSyncToolsQueueAJob(t *testing.T) {
	for _, tc := range []struct {
		tool   string
		kind   string
		action string
	}{
		{tool: "publish_kb_page_to_youtrack", kind: vault.JobKindKBPublish, action: "publish"},
		{tool: "sync_kb_page_from_youtrack", kind: vault.JobKindKBPull, action: "pull"},
	} {
		t.Run(tc.tool, func(t *testing.T) {
			h, queue := newYouTrackHarness(t, vault.YouTrackPushManual)

			got := call[KBSyncResult](t, h, tc.tool, map[string]any{
				"project": "DEMO", "path": "docs/architecture/overview.md",
			})
			if got.JobID == "" {
				t.Fatal("the result carries no job id")
			}
			if len(got.Pages) != 1 || got.Pages[0].Action != tc.action {
				t.Errorf("pages = %+v, want one %s entry", got.Pages, tc.action)
			}
			if len(queue.jobs) != 1 || queue.jobs[0].Kind != tc.kind {
				t.Errorf("queued %+v, want one %s job", queue.jobs, tc.kind)
			}
		})
	}
}

// TestKBSyncToolsGuardThePath proves the path guard runs before the core is
// asked anything: a knowledge-base path is user-supplied and the guard is what
// keeps it inside the mounted roots.
func TestKBSyncToolsGuardThePath(t *testing.T) {
	h, queue := newYouTrackHarness(t, vault.YouTrackPushManual)

	for _, tool := range []string{"publish_kb_page_to_youtrack", "sync_kb_page_from_youtrack"} {
		t.Run(tool, func(t *testing.T) {
			got := callFails(t, h, tool, map[string]any{
				"project": "DEMO", "path": "../../etc/passwd",
			})
			if got.Code != codeForbiddenPath {
				t.Errorf("code = %q, want %q", got.Code, codeForbiddenPath)
			}
			if len(queue.jobs) != 0 {
				t.Errorf("queued %+v for a path outside the vault", queue.jobs)
			}
		})
	}

	t.Run("the path is required", func(t *testing.T) {
		got := callFails(t, h, "publish_kb_page_to_youtrack", map[string]any{
			"project": "DEMO", "path": "   ",
		})
		if got.Field != "path" {
			t.Errorf("field = %q, want path", got.Field)
		}
	})
}
