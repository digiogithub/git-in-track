package mapping

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/digiogithub/git-in-track/internal/core"
	"github.com/digiogithub/git-in-track/internal/youtrack"
)

// update regenerates the golden files instead of comparing against them. Review
// the diff by hand: never regenerate blindly to make a test pass.
var update = flag.Bool("update", false, "regenerate the golden files under testdata/")

// goldenIssue is the shape written to a golden file: everything IssueToDraft
// produced, in one document, so that a change to any of it shows up in review.
type goldenIssue struct {
	Draft     core.ItemDraft `json:"draft"`
	Relations Relations      `json:"relations"`
	Warnings  []string       `json:"warnings"`
	Patch     core.ItemPatch `json:"patch"`
}

// testOptions are the options every golden case maps with: a fixed base URL and
// no synced_at, because this package never reads a clock.
func testOptions(itemID string) Options {
	return Options{BaseURL: "https://yt.example.com/youtrack", ItemID: itemID}
}

func TestIssueToDraftGolden(t *testing.T) {
	cases := []struct {
		name    string
		fixture string
		itemID  string
	}{
		{name: "epic", fixture: "epic.json", itemID: "ACME-EP-0001"},
		{name: "story with subtasks", fixture: "story.json", itemID: "ACME-US-0042"},
		{name: "bug with attachments", fixture: "bug.json", itemID: "ACME-T-0077"},
		{name: "unmapped custom fields", fixture: "unmapped.json", itemID: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			issue := loadIssue(t, tc.fixture)
			opts := testOptions(tc.itemID)
			draft, relations, warnings := IssueToDraft(issue, opts)
			patch, patchRelations, patchWarnings := IssueToPatch(issue, opts)

			if !equalJSON(t, relations, patchRelations) {
				t.Error("IssueToDraft and IssueToPatch disagree on the relations")
			}
			if !equalJSON(t, warningLines(warnings), warningLines(patchWarnings)) {
				t.Error("IssueToDraft and IssueToPatch disagree on the warnings")
			}

			got := goldenIssue{Draft: draft, Relations: relations, Warnings: warningLines(warnings), Patch: patch}
			compareGolden(t, tc.fixture[:len(tc.fixture)-len(".json")]+".golden.json", got)
		})
	}
}

func TestCommentsToDraftsGolden(t *testing.T) {
	comments := loadComments(t, "comments.json")
	drafts, warnings := CommentsToDrafts(comments, "ACME-42", testOptions("ACME-US-0042"))
	compareGolden(t, "comments.golden.json", struct {
		Drafts   []CommentDraft `json:"drafts"`
		Warnings []string       `json:"warnings"`
	}{drafts, warningLines(warnings)})
}

// compareGolden marshals a value and compares it with the golden file, or
// rewrites the file when -update is set.
func compareGolden(t *testing.T, name string, value any) {
	t.Helper()
	got, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatalf("encoding the result: %v", err)
	}
	got = append(got, '\n')
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatalf("writing %s: %v", path, err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s (run go test -update to create it): %v", path, err)
	}
	if string(got) != string(want) {
		t.Errorf("%s does not match the golden file.\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
	}
}

// loadIssue decodes one issue fixture.
func loadIssue(t *testing.T, name string) youtrack.Issue {
	t.Helper()
	var issue youtrack.Issue
	decodeFixture(t, name, &issue)
	return issue
}

// loadComments decodes one comment-list fixture.
func loadComments(t *testing.T, name string) []youtrack.Comment {
	t.Helper()
	var comments []youtrack.Comment
	decodeFixture(t, name, &comments)
	return comments
}

func decodeFixture(t *testing.T, name string, into any) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("reading the fixture %s: %v", name, err)
	}
	if err := json.Unmarshal(raw, into); err != nil {
		t.Fatalf("decoding the fixture %s: %v", name, err)
	}
}

// warningLines renders warnings for a golden file.
func warningLines(ws []Warning) []string {
	if len(ws) == 0 {
		return nil
	}
	out := make([]string, 0, len(ws))
	for _, w := range ws {
		out = append(out, w.String())
	}
	return out
}

// equalJSON compares two values by their JSON rendering, which is enough for
// the plain structs this package returns.
func equalJSON(t *testing.T, a, b any) bool {
	t.Helper()
	left, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}
	right, err := json.Marshal(b)
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}
	return string(left) == string(right)
}
