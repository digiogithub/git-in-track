package vault

import (
	"context"
	"fmt"
	"testing"

	"github.com/digiogithub/git-in-track/internal/youtrack"
)

// The knowledge-base half of GIT-US-0090, over the same fake YouTrack the
// import tests use. The two things worth proving here are that the default
// status call never reaches the network, and that the five states are decided
// by content rather than by bytes: the feedback block is rewritten on every
// local write, so a byte comparison would leave every published page
// permanently "out of date".

// kbFixture attaches the fake and a recording enqueuer to the DEMO repository.
func kbFixture(t *testing.T, w *Workspace, fake *fakeYouTrack) *fakeQueue {
	t.Helper()
	queue := &fakeQueue{}
	mount, ok := w.MountForProject("DEMO")
	if !ok {
		t.Fatal("the fixture workspace serves no DEMO project")
	}
	mount.Vault.SetYouTrackProvider(
		func(context.Context, string) (YouTrackSource, YouTrackLink, error) {
			return fake, YouTrackLink{BaseURL: "https://yt.example.com", Project: "ACME"}, nil
		})
	mount.Vault.SetYouTrackEnqueuer(queue.enqueue)
	return queue
}

// fakeQueue records the jobs the vault handed to the host instead of running
// them, which is the whole contract of the enqueue seam.
type fakeQueue struct {
	jobs []YouTrackJob
	fail error
}

func (q *fakeQueue) enqueue(_ context.Context, job YouTrackJob) (string, error) {
	if q.fail != nil {
		return "", q.fail
	}
	q.jobs = append(q.jobs, job)
	return fmt.Sprintf("job-%d", len(q.jobs)), nil
}

// keys renders the coalescing keys of the recorded jobs.
func (q *fakeQueue) keys() []string {
	out := make([]string, 0, len(q.jobs))
	for _, job := range q.jobs {
		out = append(out, job.Key)
	}
	return out
}

// writePage writes one knowledge-base page through the ordinary "kb.write"
// path, which is how a page acquires its rev and its front matter.
func writePage(t *testing.T, w *Workspace, path, text string) {
	t.Helper()
	wsCall(t, w, "kb.write", map[string]any{"project": "DEMO", "path": path, "text": text})
}

// syncedPage renders a page whose `external` entry claims the last
// synchronization published `published`, while the file now holds `local`. The
// two differ exactly when the page was edited after it was published.
func syncedPage(title, articleID, published, local string) string {
	return fmt.Sprintf(`---
title: %s
external:
  - system: youtrack
    id: %s
    url: https://yt.example.com/article/%s
    key: %s
    synced_at: 2026-09-01T10:00:00Z
---

%s
`, title, articleID, articleID, kbFingerprint(published), local)
}

// statusOf runs one status call and indexes the answer by page path.
func statusOf(t *testing.T, w *Workspace, params map[string]any) map[string]YouTrackKBPageStatus {
	t.Helper()
	result := decode[YouTrackKBStatusResult](t, wsCall(t, w, "youtrack.kb.status", params))
	out := make(map[string]YouTrackKBPageStatus, len(result.Pages))
	for _, page := range result.Pages {
		out[page.Path] = page
	}
	return out
}

func TestYouTrackKBStatusStaysOfflineByDefault(t *testing.T) {
	writableModes(t, func(t *testing.T, w *Workspace) {
		fake := &fakeYouTrack{articles: map[string]youtrack.Article{
			"DEMO-A-1": {IDReadable: "DEMO-A-1", Summary: "Synced", Content: "Body of the page."},
		}}
		kbFixture(t, w, fake)
		writePage(t, w, "docs/synced.md", syncedPage("Synced", "DEMO-A-1", "Body of the page.", "Body of the page."))

		pages := statusOf(t, w, map[string]any{"project": "DEMO", "path": "docs/synced.md"})
		got, ok := pages["docs/synced.md"]
		if !ok {
			t.Fatalf("the page is missing from the answer: %+v", pages)
		}
		if got.State != KBStateInSync {
			t.Errorf("state = %q, want %q", got.State, KBStateInSync)
		}
		if !got.Linked || got.ArticleID != "DEMO-A-1" {
			t.Errorf("linkage = %+v", got)
		}
		if fake.articleReads != 0 {
			t.Errorf("articleReads = %d: a status call without \"remote\" must not touch the network",
				fake.articleReads)
		}
	})
}

func TestYouTrackKBStatusStates(t *testing.T) {
	const body = "Body of the page."

	cases := []struct {
		name    string
		page    string
		article youtrack.Article
		remote  bool
		want    string
	}{
		{
			name: "a page with no reference is unlinked",
			page: "---\ntitle: Loose\n---\n\n" + body + "\n",
			want: KBStateUnlinked,
		},
		{
			name: "an untouched page is in sync without asking the tracker",
			page: syncedPage("Synced", "DEMO-A-1", body, body),
			want: KBStateInSync,
		},
		{
			name: "a page edited since the last publish is local ahead",
			page: syncedPage("Synced", "DEMO-A-1", body, "Body of the page, edited here."),
			want: KBStateLocalAhead,
		},
		{
			name:    "matching content is in sync even with the tracker read",
			page:    syncedPage("Synced", "DEMO-A-1", body, body),
			article: youtrack.Article{IDReadable: "DEMO-A-1", Summary: "Synced", Content: body},
			remote:  true,
			want:    KBStateInSync,
		},
		{
			name: "an article edited upstream is remote ahead",
			page: syncedPage("Synced", "DEMO-A-1", body, body),
			article: youtrack.Article{
				IDReadable: "DEMO-A-1", Summary: "Synced", Content: body + "\n\nAdded in YouTrack.",
			},
			remote: true,
			want:   KBStateRemoteAhead,
		},
		{
			name: "both sides changed is a conflict",
			page: syncedPage("Synced", "DEMO-A-1", body, "Body of the page, edited here."),
			article: youtrack.Article{
				IDReadable: "DEMO-A-1", Summary: "Synced", Content: body + "\n\nAdded in YouTrack.",
			},
			remote: true,
			want:   KBStateConflict,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			writableModes(t, func(t *testing.T, w *Workspace) {
				fake := &fakeYouTrack{articles: map[string]youtrack.Article{}}
				if tc.article.IDReadable != "" {
					fake.articles[tc.article.IDReadable] = tc.article
				}
				kbFixture(t, w, fake)
				writePage(t, w, "docs/synced.md", tc.page)

				pages := statusOf(t, w, map[string]any{
					"project": "DEMO", "path": "docs/synced.md", "remote": tc.remote,
				})
				got := pages["docs/synced.md"]
				if got.State != tc.want {
					t.Errorf("state = %q, want %q", got.State, tc.want)
				}
			})
		})
	}
}

// TestYouTrackKBStatusIgnoresTheFeedbackBlock is the case the whole comparison
// exists for: a local feedback note is not a remote change, and a page carrying
// one must not report "local ahead" forever (ADR-030, R-FB-4).
func TestYouTrackKBStatusIgnoresTheFeedbackBlock(t *testing.T) {
	const body = "Body of the page."
	const feedback = "<!-- gintrack:feedback:begin -->\n## Feedback\n\n- a note\n" +
		"<!-- gintrack:feedback:end -->"

	writableModes(t, func(t *testing.T, w *Workspace) {
		fake := &fakeYouTrack{articles: map[string]youtrack.Article{
			"DEMO-A-1": {IDReadable: "DEMO-A-1", Summary: "Synced", Content: body},
		}}
		kbFixture(t, w, fake)
		// The fingerprint is the one of the published content, which never
		// carries the block; the page on disk does.
		page := syncedPage("Synced", "DEMO-A-1", body, body)
		writePage(t, w, "docs/synced.md", page+"\n"+feedback+"\n")

		pages := statusOf(t, w, map[string]any{
			"project": "DEMO", "path": "docs/synced.md", "remote": true,
		})
		if got := pages["docs/synced.md"].State; got != KBStateInSync {
			t.Errorf("state = %q, want %q: the feedback block never leaves the repository "+
				"and is not evidence of a change", got, KBStateInSync)
		}
	})
}

func TestYouTrackKBStatusAnswersASubtreeAtOnce(t *testing.T) {
	writableModes(t, func(t *testing.T, w *Workspace) {
		kbFixture(t, w, &fakeYouTrack{})
		writePage(t, w, "docs/guide/one.md", "---\ntitle: One\n---\n\nOne.\n")
		writePage(t, w, "docs/guide/deep/two.md", "---\ntitle: Two\n---\n\nTwo.\n")

		shallow := statusOf(t, w, map[string]any{"project": "DEMO", "path": "docs/guide"})
		if _, ok := shallow["docs/guide/deep/two.md"]; ok {
			t.Error("a folder without \"recursive\" answers for its direct pages only")
		}
		if _, ok := shallow["docs/guide/one.md"]; !ok {
			t.Errorf("the direct page is missing: %+v", shallow)
		}

		deep := statusOf(t, w, map[string]any{
			"project": "DEMO", "path": "docs/guide", "recursive": true,
		})
		for _, want := range []string{"docs/guide/one.md", "docs/guide/deep/two.md"} {
			if _, ok := deep[want]; !ok {
				t.Errorf("%s is missing from the recursive answer: %+v", want, deep)
			}
		}
	})
}

func TestYouTrackKBPublishAndPullQueueAJob(t *testing.T) {
	for _, method := range []string{"youtrack.kb.publish", "youtrack.kb.pull"} {
		t.Run(method, func(t *testing.T) {
			writableModes(t, func(t *testing.T, w *Workspace) {
				queue := kbFixture(t, w, &fakeYouTrack{})
				writePage(t, w, "docs/guide/one.md", "---\ntitle: One\n---\n\nOne.\n")

				result := decode[YouTrackKBJobResult](t, wsCall(t, w, method, map[string]any{
					"project": "DEMO", "path": "docs/guide", "recursive": true,
				}))
				if result.JobID != "job-1" {
					t.Errorf("jobId = %q, want job-1", result.JobID)
				}
				if len(result.Pages) != 1 || result.Pages[0] != "docs/guide/one.md" {
					t.Errorf("pages = %v", result.Pages)
				}
				if len(queue.jobs) != 1 {
					t.Fatalf("queued %d jobs, want 1", len(queue.jobs))
				}
				if queue.jobs[0].Kind != method {
					t.Errorf("kind = %q, want %q", queue.jobs[0].Kind, method)
				}
				if queue.jobs[0].Key != "DEMO:docs/guide:recursive" {
					t.Errorf("coalescing key = %q", queue.jobs[0].Key)
				}
			})
		})
	}
}

func TestYouTrackKBQueueValidation(t *testing.T) {
	cases := []struct {
		name   string
		params map[string]any
		setup  func(t *testing.T, w *Workspace)
		want   string
	}{
		{
			name:   "a path leaving the vault is refused",
			params: map[string]any{"project": "DEMO", "path": "../etc/passwd"},
			setup:  func(t *testing.T, w *Workspace) { kbFixture(t, w, &fakeYouTrack{}) },
			want:   "invalid_request",
		},
		{
			name:   "a path matching no page is not found",
			params: map[string]any{"project": "DEMO", "path": "docs/absent.md"},
			setup:  func(t *testing.T, w *Workspace) { kbFixture(t, w, &fakeYouTrack{}) },
			want:   "not_found",
		},
		{
			name:   "an unlinked project cannot be published",
			params: map[string]any{"project": "DEMO", "path": "docs/index.md"},
			setup: func(t *testing.T, w *Workspace) {
				mount, _ := w.MountForProject("DEMO")
				mount.Vault.SetYouTrackEnqueuer((&fakeQueue{}).enqueue)
				mount.Vault.SetYouTrackProvider(nil)
			},
			want: "unavailable",
		},
		{
			name:   "a host with no engine cannot queue anything",
			params: map[string]any{"project": "DEMO", "path": "docs/index.md"},
			setup: func(t *testing.T, w *Workspace) {
				mount, _ := w.MountForProject("DEMO")
				mount.Vault.SetYouTrackProvider(
					func(context.Context, string) (YouTrackSource, YouTrackLink, error) {
						return &fakeYouTrack{}, YouTrackLink{}, nil
					})
				mount.Vault.SetYouTrackEnqueuer(nil)
			},
			want: "unavailable",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			writableModes(t, func(t *testing.T, w *Workspace) {
				tc.setup(t, w)
				code, message := wsFail(t, w, "youtrack.kb.publish", tc.params)
				if code != tc.want {
					t.Errorf("code = %q (%s), want %q", code, message, tc.want)
				}
			})
		})
	}
}
