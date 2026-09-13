package vault

import (
	"context"
	"testing"

	"github.com/digiogithub/git-in-track/internal/youtrack"
)

// The comment half of GIT-US-0079 and GIT-US-0072. Two things are proved here:
// a push is always queued and never performed inline, and the automatic seam
// lives in "comment.add" — the one path every surface goes through — so that
// REST, the web app, MCP and the CLI cannot disagree about when a comment is
// sent to the tracker.

// pushFixture attaches the fake, the recording queue and a link in the given
// push mode, and imports one issue so that DEMO holds a linked item.
func pushFixture(t *testing.T, w *Workspace, mode string) (*fakeQueue, string) {
	t.Helper()
	fake := &fakeYouTrack{issues: map[string]youtrack.Issue{
		"ACME-1": ytIssue("ACME-1", "A linked story", "User Story"),
	}}
	queue := &fakeQueue{}
	mount, ok := w.MountForProject("DEMO")
	if !ok {
		t.Fatal("the fixture workspace serves no DEMO project")
	}
	mount.Vault.SetYouTrackProvider(
		func(context.Context, string) (YouTrackSource, YouTrackLink, error) {
			return fake, YouTrackLink{
				BaseURL: "https://yt.example.com", Project: "ACME", PushComments: mode,
			}, nil
		})
	mount.Vault.SetYouTrackEnqueuer(queue.enqueue)

	result := runImport(t, w, map[string]any{"project": "DEMO", "ids": []string{"ACME-1"}})
	if len(result.Issues) != 1 || result.Issues[0].ItemID == "" {
		t.Fatalf("the import produced no linked item: %+v", result.Issues)
	}
	return queue, result.Issues[0].ItemID
}

// addComment writes one comment through the ordinary core method and returns
// its path.
func addComment(t *testing.T, w *Workspace, item, body string) string {
	t.Helper()
	out := decode[struct {
		Comment struct {
			Path string `json:"path"`
		} `json:"comment"`
	}](t, wsCall(t, w, "comment.add", map[string]any{
		"id": item, "author": "claude", "body": body, "rev": "*",
	}))
	if out.Comment.Path == "" {
		t.Fatal("comment.add answered without a path")
	}
	return out.Comment.Path
}

func TestCommentAddQueuesAPushInAutoMode(t *testing.T) {
	writableModes(t, func(t *testing.T, w *Workspace) {
		queue, item := pushFixture(t, w, YouTrackPushAuto)
		path := addComment(t, w, item, "Progress report.")

		if len(queue.jobs) != 1 {
			t.Fatalf("queued %d jobs, want exactly 1: %v", len(queue.jobs), queue.keys())
		}
		job := queue.jobs[0]
		if job.Kind != JobKindCommentPush {
			t.Errorf("kind = %q, want %q", job.Kind, JobKindCommentPush)
		}
		if job.Key != path {
			t.Errorf("coalescing key = %q, want the comment path %q", job.Key, path)
		}
	})
}

func TestCommentAddQueuesNothingInManualMode(t *testing.T) {
	writableModes(t, func(t *testing.T, w *Workspace) {
		queue, item := pushFixture(t, w, YouTrackPushManual)
		addComment(t, w, item, "Progress report.")

		if len(queue.jobs) != 0 {
			t.Errorf("queued %v: manual mode is the default and queues nothing", queue.keys())
		}
	})
}

// TestCommentAddOnAnUnlinkedItemQueuesNothing covers the quiet case: a comment
// on an item no tracker knows is an ordinary comment, not an error.
func TestCommentAddOnAnUnlinkedItemQueuesNothing(t *testing.T) {
	writableModes(t, func(t *testing.T, w *Workspace) {
		queue, _ := pushFixture(t, w, YouTrackPushAuto)
		queue.jobs = nil

		addComment(t, w, "DEMO-US-0001", "A comment on an item YouTrack never saw.")
		if len(queue.jobs) != 0 {
			t.Errorf("queued %v for an unlinked item", queue.keys())
		}
	})
}

// TestCommentPushIsCoalescedPerCommentPath proves the key the engine folds two
// queued jobs on is the comment path, so a burst on one comment collapses and
// two different comments stay two jobs.
func TestCommentPushIsCoalescedPerCommentPath(t *testing.T) {
	writableModes(t, func(t *testing.T, w *Workspace) {
		queue, item := pushFixture(t, w, YouTrackPushAuto)
		first := addComment(t, w, item, "One.")
		second := addComment(t, w, item, "Two.")

		if first == second {
			t.Fatal("two comments landed on the same path")
		}
		keys := queue.keys()
		if len(keys) != 2 || keys[0] != first || keys[1] != second {
			t.Errorf("keys = %v, want one per comment path (%q, %q)", keys, first, second)
		}
	})
}

func TestYouTrackCommentPushSelection(t *testing.T) {
	t.Run("all selects every comment that is not upstream yet", func(t *testing.T) {
		writableModes(t, func(t *testing.T, w *Workspace) {
			queue, item := pushFixture(t, w, YouTrackPushManual)
			first := addComment(t, w, item, "One.")
			second := addComment(t, w, item, "Two.")

			result := decode[YouTrackCommentPushResult](t,
				wsCall(t, w, "youtrack.comment.push", map[string]any{
					"project": "DEMO", "itemId": item, "all": true,
				}))
			if len(result.Pushed) != 2 {
				t.Fatalf("pushed = %+v, want both comments", result.Pushed)
			}
			seen := map[string]bool{}
			for _, entry := range result.Pushed {
				seen[entry.CommentPath] = true
			}
			if !seen[first] || !seen[second] {
				t.Errorf("pushed = %+v, want %q and %q", result.Pushed, first, second)
			}
			if result.JobID == "" {
				t.Error("the result carries no job id")
			}
			if len(queue.jobs) != 2 {
				t.Errorf("queued %v, want one job per comment", queue.keys())
			}
		})
	})

	t.Run("commentPath selects one comment", func(t *testing.T) {
		writableModes(t, func(t *testing.T, w *Workspace) {
			queue, item := pushFixture(t, w, YouTrackPushManual)
			addComment(t, w, item, "One.")
			second := addComment(t, w, item, "Two.")

			result := decode[YouTrackCommentPushResult](t,
				wsCall(t, w, "youtrack.comment.push", map[string]any{
					"project": "DEMO", "itemId": item, "commentPath": second,
				}))
			if len(result.Pushed) != 1 || result.Pushed[0].CommentPath != second {
				t.Errorf("pushed = %+v, want only %q", result.Pushed, second)
			}
			if len(queue.jobs) != 1 {
				t.Errorf("queued %v, want one job", queue.keys())
			}
		})
	})
}

func TestYouTrackCommentPushValidation(t *testing.T) {
	cases := []struct {
		name   string
		params map[string]any
		want   string
	}{
		{
			name:   "the item is required",
			params: map[string]any{"project": "DEMO", "all": true},
			want:   "invalid_request",
		},
		{
			name:   "one selection mode or the other, not neither",
			params: map[string]any{"project": "DEMO", "itemId": "DEMO-US-0001"},
			want:   "invalid_request",
		},
		{
			name: "one selection mode or the other, not both",
			params: map[string]any{
				"project": "DEMO", "itemId": "DEMO-US-0001",
				"commentPath": "docs/.pmngr/comments/x.md", "all": true,
			},
			want: "invalid_request",
		},
		{
			name:   "an item with no YouTrack reference cannot be pushed",
			params: map[string]any{"project": "DEMO", "itemId": "DEMO-US-0001", "all": true},
			want:   "invalid_request",
		},
		{
			name:   "an unknown item is not found",
			params: map[string]any{"project": "DEMO", "itemId": "DEMO-US-9999", "all": true},
			want:   "not_found",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			writableModes(t, func(t *testing.T, w *Workspace) {
				pushFixture(t, w, YouTrackPushManual)
				code, message := wsFail(t, w, "youtrack.comment.push", tc.params)
				if code != tc.want {
					t.Errorf("code = %q (%s), want %q", code, message, tc.want)
				}
			})
		})
	}
}

func TestYouTrackCommentPushNeedsAnEngine(t *testing.T) {
	writableModes(t, func(t *testing.T, w *Workspace) {
		_, item := pushFixture(t, w, YouTrackPushManual)
		mount, _ := w.MountForProject("DEMO")
		mount.Vault.SetYouTrackEnqueuer(nil)

		code, _ := wsFail(t, w, "youtrack.comment.push", map[string]any{
			"project": "DEMO", "itemId": item, "all": true,
		})
		if code != "unavailable" {
			t.Errorf("code = %q, want unavailable", code)
		}
	})
}
