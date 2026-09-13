package server

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/digiogithub/git-in-track/internal/vault"
	"github.com/digiogithub/git-in-track/internal/youtrack"
)

// The knowledge-base publish and pull, story GIT-US-0087.

// anchorLine is the line of the fixture index page every test note is anchored
// to. A feedback note follows the text it was attached to and is pruned when
// that text disappears (ADR-030), so a test that expects the block to survive a
// pull has to anchor it to a line the incoming article still carries — which is
// what anchorParagraph is for.
const anchorLine = 3

// anchorParagraph is the line of docs/index.md the notes are anchored to.
const anchorParagraph = "This folder is the documentation root of the fixture project used by the"

// The two pages of the fixture knowledge base: one at the root of the
// documentation folder and one a level down, which is what makes the folder
// tree of a publish observable.
const (
	kbIndexPage    = "docs/index.md"
	kbOverviewPage = "docs/architecture/overview.md"
)

// runKB runs one publish or pull over the fixture.
func runKB(
	ctx context.Context, t *testing.T, s *Server, direction string, params vault.YouTrackKBParams,
) error {
	t.Helper()

	handler, kind := s.handleYouTrackKBPublishJobs, kindYouTrackKBPublish
	if direction == kbDirectionPull {
		handler, kind = s.handleYouTrackKBPullJobs, kindYouTrackKBPull
	}
	return runJob(ctx, t, handler, kind,
		youtrackJobPayload{Repo: testRepoID, Project: "DEMO"}, params)
}

// readFile is the bytes of one vault-relative path.
func readFile(t *testing.T, root, rel string) string {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(data)
}

// conflictEvents collects the `youtrack.kb.conflict` payloads the hub buffered.
func conflictEvents(t *testing.T, s *Server) []kbConflictEventData {
	t.Helper()

	events, _ := s.hub.since(0)
	out := make([]kbConflictEventData, 0, len(events))
	for _, ev := range events {
		if ev.Type != eventYouTrackKBConflict {
			continue
		}
		if data, ok := ev.Data.(kbConflictEventData); ok {
			out = append(out, data)
		}
	}
	return out
}

// TestKBPublishCreatesThenUpdates covers the first acceptance criterion of
// GIT-US-0087: an unlinked page is created with `project` in the creation body,
// the article id and url are written back into its front matter, and a second
// publish of the same page updates the article it now names instead of creating
// another one.
func TestKBPublishCreatesThenUpdates(t *testing.T) {
	t.Parallel()

	fake := newFakeYouTrack()
	s, root := newJobServer(t, fake)

	if err := runKB(t.Context(), t, s, kbDirectionPublish,
		vault.YouTrackKBParams{Project: "DEMO", Path: kbIndexPage}); err != nil {
		t.Fatalf("the publish failed: %v", err)
	}
	if len(fake.createdArts) != 1 {
		t.Fatalf("the publish created %d articles, want 1", len(fake.createdArts))
	}
	if fake.createdArts[0].ProjectID != "DEMO" {
		t.Errorf("the creation body carries project %q, want DEMO: a project is only settable at creation",
			fake.createdArts[0].ProjectID)
	}
	if fake.createdArts[0].Summary == nil || *fake.createdArts[0].Summary == "" {
		t.Error("the article was created without a summary")
	}

	written := readFile(t, root, kbIndexPage)
	for _, want := range []string{"external:", "system: youtrack", "id: DEMO-A-1",
		"https://yt.example.com/youtrack/article/DEMO-A-1", "synced_at:"} {
		if !strings.Contains(written, want) {
			t.Errorf("the page does not record %q:\n%s", want, written)
		}
	}
	if !strings.Contains(written, "Demo Shop knowledge base") {
		t.Error("the publish lost the page body")
	}

	// A second publish of an unchanged page still resolves to the article the
	// page now names, and never creates a second one.
	if err := runKB(t.Context(), t, s, kbDirectionPublish,
		vault.YouTrackKBParams{Project: "DEMO", Path: kbIndexPage}); err != nil {
		t.Fatalf("the second publish failed: %v", err)
	}
	if len(fake.createdArts) != 1 {
		t.Errorf("the second publish created another article: %d in total", len(fake.createdArts))
	}
}

// TestKBPublishUpdatesAChangedPage proves the update path: a page whose content
// moved since it was published is sent to POST /api/articles/{id}.
func TestKBPublishUpdatesAChangedPage(t *testing.T) {
	t.Parallel()

	fake := newFakeYouTrack()
	s, root := newJobServer(t, fake)

	if err := runKB(t.Context(), t, s, kbDirectionPublish,
		vault.YouTrackKBParams{Project: "DEMO", Path: kbIndexPage}); err != nil {
		t.Fatalf("the publish failed: %v", err)
	}
	editPage(t, s, root, kbIndexPage, "\nA local paragraph nobody else has.\n")

	if err := runKB(t.Context(), t, s, kbDirectionPublish,
		vault.YouTrackKBParams{Project: "DEMO", Path: kbIndexPage}); err != nil {
		t.Fatalf("the second publish failed: %v", err)
	}
	if len(fake.updatedArts) != 1 || fake.updatedArts[0] != "DEMO-A-1" {
		t.Fatalf("updates = %v, want one update of DEMO-A-1", fake.updatedArts)
	}
	if got := fake.articles["DEMO-A-1"].Content; !strings.Contains(got, "A local paragraph") {
		t.Errorf("the article did not receive the local edit:\n%s", got)
	}
}

// editPage appends a paragraph to a page and re-indexes the mount, which is
// what a user editing in the web app would produce.
func editPage(t *testing.T, s *Server, root, rel, extra string) {
	t.Helper()

	file := filepath.Join(root, filepath.FromSlash(rel))
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	if err := os.WriteFile(file, append(data, []byte(extra)...), 0o600); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
	m, ok := s.repos.lookup(testRepoID)
	if !ok {
		t.Fatal("the fixture repository is not mounted")
	}
	if _, err := m.reindex(t.Context(), s.now); err != nil {
		t.Fatalf("reindex: %v", err)
	}
}

// TestKBPublishBuildsTheParentTree is the folder contract: a recursive publish
// creates one parent article per directory, publishes it before the pages under
// it, and reuses it on the next run rather than creating a second one.
//
// Ordering comes from the order pages are published in, never from `ordinal`,
// which YouTrack documents as read-only and silently ignores when sent.
func TestKBPublishBuildsTheParentTree(t *testing.T) {
	t.Parallel()

	fake := newFakeYouTrack()
	s, _ := newJobServer(t, fake)

	err := runKB(t.Context(), t, s, kbDirectionPublish,
		vault.YouTrackKBParams{Project: "DEMO", Recursive: true})
	if err != nil {
		t.Fatalf("the folder publish failed: %v", err)
	}

	// Three creations: the parent article of docs/architecture, and the two
	// pages. The parent comes before the page that hangs under it.
	var parentAt, childAt = -1, -1
	for i, in := range fake.createdArts {
		if in.Summary == nil {
			continue
		}
		switch *in.Summary {
		case "architecture":
			parentAt = i
		case "Architecture overview":
			childAt = i
		}
	}
	if parentAt < 0 {
		t.Fatalf("no parent article was created for docs/architecture: %d creations", len(fake.createdArts))
	}
	if childAt < 0 {
		t.Fatal("the page under docs/architecture was not published")
	}
	if parentAt > childAt {
		t.Errorf("the parent was created after its child: parent at %d, child at %d", parentAt, childAt)
	}
	if in := fake.createdArts[childAt]; in.ParentArticleID == nil || *in.ParentArticleID == "" {
		t.Error("the child article was created without a parentArticle")
	}
	if in := fake.createdArts[parentAt]; in.ParentArticleID != nil && *in.ParentArticleID != "" {
		t.Errorf("a top-level folder was given a parent: %q", *in.ParentArticleID)
	}

	before := len(fake.createdArts)
	if err := runKB(t.Context(), t, s, kbDirectionPublish,
		vault.YouTrackKBParams{Project: "DEMO", Recursive: true}); err != nil {
		t.Fatalf("the second folder publish failed: %v", err)
	}
	if len(fake.createdArts) != before {
		t.Errorf("the re-publish created %d more articles instead of reusing the tree",
			len(fake.createdArts)-before)
	}
}

// TestKBPublishSearchesForAParentWithAnOrderingClause pins the paging rule for
// article listings: the search that looks for an existing top-level parent
// always carries an ordering clause, because a $skip walk without one skips and
// duplicates rows.
func TestKBPublishSearchesForAParentWithAnOrderingClause(t *testing.T) {
	t.Parallel()

	fake := newFakeYouTrack()
	s, _ := newJobServer(t, fake)

	if err := runKB(t.Context(), t, s, kbDirectionPublish,
		vault.YouTrackKBParams{Project: "DEMO", Recursive: true}); err != nil {
		t.Fatalf("the folder publish failed: %v", err)
	}
	if len(fake.queries) == 0 {
		t.Fatal("no article search was made")
	}
	for _, query := range fake.queries {
		if !strings.Contains(strings.ToLower(query), "order by:") {
			t.Errorf("query %q carries no ordering clause", query)
		}
	}
}

// TestKBPublishIsNotRecursiveByDefault proves a folder publish answers for its
// direct pages only unless the caller asked for the subtree.
func TestKBPublishIsNotRecursiveByDefault(t *testing.T) {
	t.Parallel()

	fake := newFakeYouTrack()
	s, _ := newJobServer(t, fake)

	if err := runKB(t.Context(), t, s, kbDirectionPublish,
		vault.YouTrackKBParams{Project: "DEMO"}); err != nil {
		t.Fatalf("the folder publish failed: %v", err)
	}
	if len(fake.createdArts) != 1 {
		t.Fatalf("a non-recursive publish created %d articles, want 1", len(fake.createdArts))
	}
	if summary := fake.createdArts[0].Summary; summary == nil || *summary == "architecture" {
		t.Error("a non-recursive publish walked into a subfolder")
	}
}

// TestKBPullWritesTheArticleBack covers the pull: an article that moved since
// the page was published is written through the vault, and the page's local
// feedback block survives it untouched.
func TestKBPullWritesTheArticleBack(t *testing.T) {
	t.Parallel()

	fake := newFakeYouTrack()
	s, root := newJobServer(t, fake)

	if err := runKB(t.Context(), t, s, kbDirectionPublish,
		vault.YouTrackKBParams{Project: "DEMO", Path: kbIndexPage}); err != nil {
		t.Fatalf("the publish failed: %v", err)
	}
	addFeedbackBlock(t, s, root, kbIndexPage)

	// The article moved and the page did not: the remote side wins.
	article := fake.articles["DEMO-A-1"]
	article.Content = "# Demo Shop knowledge base\n\n" + anchorParagraph +
		"\n\nRewritten in YouTrack by somebody else.\n"
	fake.articles["DEMO-A-1"] = article

	if err := runKB(t.Context(), t, s, kbDirectionPull,
		vault.YouTrackKBParams{Project: "DEMO", Path: kbIndexPage}); err != nil {
		t.Fatalf("the pull failed: %v", err)
	}
	written := readFile(t, root, kbIndexPage)
	if !strings.Contains(written, "Rewritten in YouTrack") {
		t.Errorf("the pull did not write the article back:\n%s", written)
	}
	if !strings.Contains(written, "gintrack:feedback:note") {
		t.Errorf("the pull dropped the local feedback block:\n%s", written)
	}
	if len(conflictEvents(t, s)) != 0 {
		t.Error("a one-sided change was reported as a conflict")
	}
}

// addFeedbackBlock attaches a real feedback note to a page through the vault,
// which is the local-only content a pull must preserve and change detection
// must ignore (ADR-030). It goes through `kb.feedback.add` rather than writing
// the markers by hand, so the block is anchored the way core anchors one and
// survives the pruning every write performs.
func addFeedbackBlock(t *testing.T, s *Server, _, rel string) {
	t.Helper()

	m, ok := s.repos.lookup(testRepoID)
	if !ok {
		t.Fatal("the fixture repository is not mounted")
	}
	run := &kbRun{mount: m, project: "DEMO", docs: "docs"}
	page, rev, _, err := s.readKBPage(t.Context(), run, rel)
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	lines := strings.Split(page.Body, "\n")
	if len(lines) < anchorLine {
		t.Fatalf("%s is too short to anchor a note on line %d", rel, anchorLine)
	}
	err = dispatchVault(t.Context(), m, "kb.feedback.add", map[string]any{
		"path": rel, "rev": rev, "author": "marta", "authorName": "Marta Ruiz",
		"notes": []map[string]any{{
			"startLine": anchorLine, "endLine": anchorLine,
			"quote": lines[anchorLine-1], "note": "a local note",
		}},
	}, nil)
	if err != nil {
		t.Fatalf("add feedback to %s: %v", rel, err)
	}
	if _, err := m.reindex(t.Context(), s.now); err != nil {
		t.Fatalf("reindex: %v", err)
	}
}

// TestKBPullLeavesALocalEditAlone is the other one-sided case: the page moved
// and the article did not, so a pull writes nothing rather than undoing the
// local edit.
func TestKBPullLeavesALocalEditAlone(t *testing.T) {
	t.Parallel()

	fake := newFakeYouTrack()
	s, root := newJobServer(t, fake)

	if err := runKB(t.Context(), t, s, kbDirectionPublish,
		vault.YouTrackKBParams{Project: "DEMO", Path: kbIndexPage}); err != nil {
		t.Fatalf("the publish failed: %v", err)
	}
	editPage(t, s, root, kbIndexPage, "\nA local paragraph nobody else has.\n")

	if err := runKB(t.Context(), t, s, kbDirectionPull,
		vault.YouTrackKBParams{Project: "DEMO", Path: kbIndexPage}); err != nil {
		t.Fatalf("the pull failed: %v", err)
	}
	written := readFile(t, root, kbIndexPage)
	if !strings.Contains(written, "A local paragraph nobody else has.") {
		t.Errorf("the pull undid the local edit:\n%s", written)
	}
	if len(conflictEvents(t, s)) != 0 {
		t.Error("a one-sided change was reported as a conflict")
	}
}

// TestKBBothChangedWritesAConflictPage is the acceptance criterion the whole
// story turns on: when both sides moved, the incoming content lands beside the
// page, the page itself is untouched, and `youtrack.kb.conflict` says so.
func TestKBBothChangedWritesAConflictPage(t *testing.T) {
	t.Parallel()

	for _, direction := range []string{kbDirectionPull, kbDirectionPublish} {
		t.Run("during a "+direction, func(t *testing.T) {
			t.Parallel()

			fake := newFakeYouTrack()
			s, root := newJobServer(t, fake)

			if err := runKB(t.Context(), t, s, kbDirectionPublish,
				vault.YouTrackKBParams{Project: "DEMO", Path: kbIndexPage}); err != nil {
				t.Fatalf("the publish failed: %v", err)
			}
			editPage(t, s, root, kbIndexPage, "\nA local paragraph nobody else has.\n")
			article := fake.articles["DEMO-A-1"]
			article.Content = "# Demo Shop knowledge base\n\nAnd a remote paragraph.\n"
			fake.articles["DEMO-A-1"] = article
			before := readFile(t, root, kbIndexPage)

			if err := runKB(t.Context(), t, s, direction,
				vault.YouTrackKBParams{Project: "DEMO", Path: kbIndexPage}); err != nil {
				t.Fatalf("the %s failed: %v", direction, err)
			}

			if after := readFile(t, root, kbIndexPage); after != before {
				t.Errorf("the page was rewritten although both sides had changed:\n%s", after)
			}
			conflict := readFile(t, root, "docs/index"+conflictSuffix)
			if !strings.Contains(conflict, "And a remote paragraph.") {
				t.Errorf("the conflict page does not carry the incoming content:\n%s", conflict)
			}
			if !strings.Contains(conflict, "conflict_of: "+kbIndexPage) {
				t.Errorf("the conflict page does not name the page it belongs to:\n%s", conflict)
			}
			events := conflictEvents(t, s)
			if len(events) != 1 {
				t.Fatalf("the job published %d conflict events, want 1", len(events))
			}
			if events[0].Path != kbIndexPage || events[0].Direction != direction {
				t.Errorf("event = %+v, want %s during a %s", events[0], kbIndexPage, direction)
			}
			if events[0].ArticleID != "DEMO-A-1" {
				t.Errorf("the event names article %q, want DEMO-A-1", events[0].ArticleID)
			}
			if direction == kbDirectionPublish && len(fake.updatedArts) != 0 {
				t.Errorf("the publish overwrote the article anyway: %v", fake.updatedArts)
			}
		})
	}
}

// TestKBFeedbackIsNotALocalChange is the rule ADR-030 forces on change
// detection: the feedback block is local-only and is rewritten on every write,
// so adding a note must not register as a content change and must not turn the
// next sync into a conflict.
func TestKBFeedbackIsNotALocalChange(t *testing.T) {
	t.Parallel()

	fake := newFakeYouTrack()
	s, root := newJobServer(t, fake)

	if err := runKB(t.Context(), t, s, kbDirectionPublish,
		vault.YouTrackKBParams{Project: "DEMO", Path: kbIndexPage}); err != nil {
		t.Fatalf("the publish failed: %v", err)
	}
	addFeedbackBlock(t, s, root, kbIndexPage)
	article := fake.articles["DEMO-A-1"]
	article.Content = "# Demo Shop knowledge base\n\nRewritten remotely.\n"
	fake.articles["DEMO-A-1"] = article

	if err := runKB(t.Context(), t, s, kbDirectionPull,
		vault.YouTrackKBParams{Project: "DEMO", Path: kbIndexPage}); err != nil {
		t.Fatalf("the pull failed: %v", err)
	}
	if events := conflictEvents(t, s); len(events) != 0 {
		t.Fatalf("a feedback note was counted as a local change: %+v", events)
	}
	if written := readFile(t, root, kbIndexPage); !strings.Contains(written, "Rewritten remotely.") {
		t.Errorf("the remote change was refused:\n%s", written)
	}
}

// TestKBPullSkipsAnUnlinkedPage proves a page that mirrors no article is a
// no-op rather than a failure: a folder pull walks pages that were never
// published and must not abandon the rest of the tree over them.
func TestKBPullSkipsAnUnlinkedPage(t *testing.T) {
	t.Parallel()

	fake := newFakeYouTrack()
	s, root := newJobServer(t, fake)
	before := readFile(t, root, kbIndexPage)

	if err := runKB(t.Context(), t, s, kbDirectionPull,
		vault.YouTrackKBParams{Project: "DEMO", Recursive: true}); err != nil {
		t.Fatalf("the pull failed: %v", err)
	}
	if after := readFile(t, root, kbIndexPage); after != before {
		t.Error("an unlinked page was rewritten by a pull")
	}
}

// TestKBSelectionRespectsTheRoot covers the page selection on its own, which is
// what decides whether a folder publish is one page or a handbook.
func TestKBSelectionRespectsTheRoot(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		page      string
		root      string
		recursive bool
		want      bool
	}{
		{name: "a direct page of the root", page: "docs/index.md", root: "docs", want: true},
		{name: "a nested page without recursion", page: "docs/a/b.md", root: "docs"},
		{name: "a nested page with recursion", page: "docs/a/b.md", root: "docs", recursive: true, want: true},
		{name: "a page outside the root", page: "other/x.md", root: "docs"},
		{name: "an empty root takes the top level", page: "index.md", want: true},
		{name: "an empty root without recursion stops at the top", page: "a/b.md"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := inKBSelection(tc.page, tc.root, tc.recursive); got != tc.want {
				t.Errorf("inKBSelection(%q, %q, %v) = %v, want %v",
					tc.page, tc.root, tc.recursive, got, tc.want)
			}
		})
	}
}

// TestKBPublishFailsTerminallyOnARejectedToken pins the classification of a
// failure that applies to every remaining page: the job stops instead of
// producing a hundred more refused requests.
func TestKBPublishFailsTerminallyOnARejectedToken(t *testing.T) {
	t.Parallel()

	fake := newFakeYouTrack()
	fake.err = &youtrack.APIError{Status: 401, Method: "POST", Path: "/api/articles"}
	s, _ := newJobServer(t, fake)

	err := runKB(t.Context(), t, s, kbDirectionPublish,
		vault.YouTrackKBParams{Project: "DEMO", Recursive: true})
	if err == nil {
		t.Fatal("a publish against a rejected token succeeded")
	}
}
