package server

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/digiogithub/git-in-track/internal/vault"
)

// ytUnlinkBody is the answer of POST …/youtrack/kb/unlink.
type ytUnlinkBody struct {
	Project   string `json:"project"`
	Path      string `json:"path"`
	Unlinked  bool   `json:"unlinked"`
	ArticleID string `json:"articleId"`
}

// TestKBUnlinkForgetsTheArticle is the story of GIT-US-0095 end to end: a page
// that was published carries an `external:` entry, unlinking removes it and
// nothing else, and a publish afterwards creates a new article rather than
// updating the one the page had forgotten.
func TestKBUnlinkForgetsTheArticle(t *testing.T) {
	t.Parallel()

	fake := newFakeYouTrack()
	s, root := newJobServer(t, fake)

	if err := runKB(t.Context(), t, s, kbDirectionPublish,
		vault.YouTrackKBParams{Project: "DEMO", Path: kbIndexPage}); err != nil {
		t.Fatalf("the publish failed: %v", err)
	}
	if written := readFile(t, root, kbIndexPage); !strings.Contains(written, "system: youtrack") {
		t.Fatalf("the page was not linked by the publish:\n%s", written)
	}

	var body ytUnlinkBody
	decode(t, send(t, s, request{
		method: http.MethodPost,
		target: "/api/v1/youtrack/kb/unlink?key=DEMO",
		body:   map[string]any{"path": kbIndexPage},
	}), http.StatusOK, &body)
	if !body.Unlinked || body.ArticleID != "DEMO-A-1" {
		t.Fatalf("unlink answered %+v", body)
	}

	written := readFile(t, root, kbIndexPage)
	for _, gone := range []string{"external:", "system: youtrack", "DEMO-A-1"} {
		if strings.Contains(written, gone) {
			t.Errorf("the page still carries %q:\n%s", gone, written)
		}
	}
	// Local and only local: the article is left where it is, and the body of
	// the page is not collateral damage.
	if !strings.Contains(written, "Demo Shop knowledge base") {
		t.Errorf("the unlink lost the page body:\n%s", written)
	}
	if len(fake.updatedArts) != 0 || len(fake.createdArts) != 1 {
		t.Errorf("the unlink talked to the instance: created=%d updated=%v",
			len(fake.createdArts), fake.updatedArts)
	}

	// Unlinking again is an answer, not a failure: the page is already in the
	// state that was asked for.
	body = ytUnlinkBody{}
	decode(t, send(t, s, request{
		method: http.MethodPost,
		target: "/api/v1/youtrack/kb/unlink?key=DEMO",
		body:   map[string]any{"path": kbIndexPage},
	}), http.StatusOK, &body)
	if body.Unlinked {
		t.Errorf("a second unlink reported a write: %+v", body)
	}

	// The connection itself is untouched, so publishing starts over: a new
	// article, not an update of the forgotten one.
	if err := runKB(t.Context(), t, s, kbDirectionPublish,
		vault.YouTrackKBParams{Project: "DEMO", Path: kbIndexPage}); err != nil {
		t.Fatalf("the publish after the unlink failed: %v", err)
	}
	if len(fake.createdArts) != 2 {
		t.Errorf("the publish after the unlink created %d articles in total, want 2",
			len(fake.createdArts))
	}
}

// TestKBUnlinkNeedsAPath pins the one refusal: the route names a page, never a
// folder, so a missing path is a bad request rather than a silent no-op.
func TestKBUnlinkNeedsAPath(t *testing.T) {
	t.Parallel()

	s, _ := newJobServer(t, newFakeYouTrack())
	var problem problemBody
	decode(t, send(t, s, request{
		method: http.MethodPost,
		target: "/api/v1/youtrack/kb/unlink?key=DEMO",
		body:   map[string]any{},
	}), http.StatusBadRequest, &problem)
	if problem.Code != codeInvalidRequest {
		t.Errorf("code = %q, want %q", problem.Code, codeInvalidRequest)
	}
}

// TestItemUnlinkForgetsOneSystem covers the item half of GIT-US-0095. An item
// imported from YouTrack carries an `external:` entry, and forgetting it is a
// set operation on that one system: an item that also mirrors another tracker
// keeps the other entry, which `unset: ["external"]` could not express.
func TestItemUnlinkForgetsOneSystem(t *testing.T) {
	t.Parallel()

	root := copyTree(t, fixtureRoot)
	linkStoryToIssue(t, root, "ACME-42")
	appendExternalEntry(t, root, "jira", "OPS-7")
	s, _ := newJobServerIn(t, newFakeYouTrack(), root)

	const id = "DEMO-US-0001"
	rev, _ := getItem(t, s, id)

	var unlinked struct {
		External []struct {
			System string `json:"system"`
			ID     string `json:"id"`
		} `json:"external"`
	}
	decode(t, send(t, s, request{
		method: http.MethodPatch,
		target: "/api/v1/items/" + id,
		header: map[string]string{"If-Match": rev},
		// No id: every reference of that system goes, which is what a person
		// asking to unlink YouTrack means.
		body: map[string]any{"removeExternal": []map[string]any{{"system": "youtrack"}}},
	}), http.StatusOK, &unlinked)

	if len(unlinked.External) != 1 || unlinked.External[0].System != "jira" {
		t.Fatalf("external = %+v, want the other tracker kept", unlinked.External)
	}
	if written := readFile(t, root, commentStoryPath); strings.Contains(written, "ACME-42") {
		t.Errorf("the file still names the issue:\n%s", written)
	}
}

// appendExternalEntry adds a second tracker to the fixture story, so a test can
// prove that unlinking one system leaves the others where they are.
func appendExternalEntry(t *testing.T, root, system, id string) {
	t.Helper()

	file := filepath.Join(root, filepath.FromSlash(commentStoryPath))
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read the story: %v", err)
	}
	text := string(data)
	marker := "external:\n"
	at := strings.Index(text, marker)
	if at < 0 {
		t.Fatal("the fixture story carries no external block")
	}
	entry := "  - system: " + system + "\n    id: " + id + "\n"
	at += len(marker)
	if err := os.WriteFile(file, []byte(text[:at]+entry+text[at:]), 0o600); err != nil {
		t.Fatalf("write the story: %v", err)
	}
}
