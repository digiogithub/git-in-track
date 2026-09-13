package vault

import (
	"encoding/json"
	"testing"

	"github.com/digiogithub/git-in-track/internal/core"
)

// commentUpdateResult is what "comment.update" answers.
type commentUpdateResult struct {
	Comment core.Comment `json:"comment"`
	Writes  WriteSet     `json:"writes"`
}

// seedComment adds one comment to the fixture thread and returns it, so that
// every case below starts from a comment this test wrote and knows the rev of.
func seedComment(t *testing.T, v *Vault, body string) core.Comment {
	t.Helper()
	added := decode[struct {
		Comment core.Comment `json:"comment"`
	}](t, call(t, v, "comment.add", map[string]any{
		"id": "DEMO-US-0001", "author": "claude", "body": body,
	}))
	if added.Comment.Path == "" || added.Comment.Rev == "" {
		t.Fatalf("comment.add answered %+v, want a path and a rev", added.Comment)
	}
	return added.Comment
}

// TestCommentUpdateRecordsAnExternalReference is the case the YouTrack comment
// push needs: after posting a comment it knows the remote id and must record it
// on the local file, rev-guarded, through the vault — not by opening the file
// itself. The write set it gets back is what commit-on-save and the browser
// host persist, and the index is already folded forward when it returns.
func TestCommentUpdateRecordsAnExternalReference(t *testing.T) {
	v, _ := loadedVault(t)
	comment := seedComment(t, v, "Pushed upstream.")

	updated := decode[commentUpdateResult](t, call(t, v, "comment.update", map[string]any{
		"id": "DEMO-US-0001", "path": comment.Path, "rev": string(comment.Rev),
		"setExternal": []map[string]any{{
			"system": "youtrack",
			"id":     "4-90",
			"url":    "https://yt.example.com/issue/ACME-42#focus=Comments-4-90",
		}},
	}))

	if len(updated.Comment.External) != 1 {
		t.Fatalf("external = %+v, want exactly one reference", updated.Comment.External)
	}
	ref := updated.Comment.External[0]
	if ref.System != "youtrack" || ref.ID != "4-90" {
		t.Errorf("external = %+v", ref)
	}
	if updated.Comment.Body != "Pushed upstream." {
		t.Errorf("body = %q, want the text untouched", updated.Comment.Body)
	}
	if updated.Comment.Rev == comment.Rev {
		t.Error("the rev did not move, so nothing was written")
	}
	if len(updated.Writes.Written) != 1 || updated.Writes.Written[0].Path != comment.Path {
		t.Fatalf("writes = %+v, want the one comment file", updated.Writes.Written)
	}

	// The index answers from the new file without any reload: that is the
	// difference between this method and a caller writing the file itself.
	thread := decode[[]core.Comment](t, call(t, v, "comment.list", map[string]any{"id": "DEMO-US-0001"}))
	var found *core.Comment
	for i := range thread {
		if thread[i].Path == comment.Path {
			found = &thread[i]
		}
	}
	if found == nil {
		t.Fatalf("the comment left the thread: %+v", thread)
	}
	if len(found.External) != 1 || found.External[0].ID != "4-90" {
		t.Errorf("the indexed comment carries %+v, want the reference just written", found.External)
	}
}

// TestCommentUpdateUpsertsOneEntryPerSystem asserts the property that makes a
// re-delivered push idempotent: pushing twice replaces the reference of that
// system instead of growing the list, and a reference to another system is left
// alone.
func TestCommentUpdateUpsertsOneEntryPerSystem(t *testing.T) {
	v, _ := loadedVault(t)
	comment := seedComment(t, v, "Pushed upstream.")

	first := decode[commentUpdateResult](t, call(t, v, "comment.update", map[string]any{
		"path": comment.Path, "rev": string(comment.Rev),
		"setExternal": []map[string]any{
			{"system": "jira", "id": "JRA-1"},
			{"system": "youtrack", "id": "4-90"},
		},
	}))
	if len(first.Comment.External) != 2 {
		t.Fatalf("external = %+v, want both systems", first.Comment.External)
	}

	second := decode[commentUpdateResult](t, call(t, v, "comment.update", map[string]any{
		"path": comment.Path, "rev": string(first.Comment.Rev),
		"setExternal": []map[string]any{{"system": "youtrack", "id": "4-91"}},
	}))
	if len(second.Comment.External) != 2 {
		t.Fatalf("external = %+v, want the youtrack entry replaced, not appended",
			second.Comment.External)
	}
	bySystem := map[string]string{}
	for _, ref := range second.Comment.External {
		bySystem[ref.System] = ref.ID
	}
	if bySystem["youtrack"] != "4-91" {
		t.Errorf("youtrack = %q, want the new remote id", bySystem["youtrack"])
	}
	if bySystem["jira"] != "JRA-1" {
		t.Errorf("jira = %q, want the untouched entry of another system", bySystem["jira"])
	}
}

// TestCommentUpdateRefusesAStaleRev is the whole reason the method takes a rev:
// a comment edited while a push was in flight must be reported, never
// clobbered, and the refusal must hand back the current rev so the caller's
// retry needs no second round trip.
func TestCommentUpdateRefusesAStaleRev(t *testing.T) {
	v, _ := loadedVault(t)
	comment := seedComment(t, v, "First draft.")

	edited := decode[commentUpdateResult](t, call(t, v, "comment.update", map[string]any{
		"path": comment.Path, "rev": string(comment.Rev), "body": "Second draft.",
	}))

	env := rawCall(t, v, "comment.update", map[string]any{
		"path": comment.Path, "rev": string(comment.Rev),
		"setExternal": []map[string]any{{"system": "youtrack", "id": "4-90"}},
	})
	if env.OK {
		t.Fatal("a write quoting the rev of a file that has moved was accepted")
	}
	if env.Error.Code != core.StaleRevisionCode {
		t.Fatalf("code = %q, want %q", env.Error.Code, core.StaleRevisionCode)
	}
	var withCurrent struct {
		Error struct {
			Current string `json:"currentRev"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(v.Call("comment.update", mustJSON(t, map[string]any{
		"path": comment.Path, "rev": string(comment.Rev), "body": "Third draft.",
	}))), &withCurrent); err != nil {
		t.Fatalf("decode the failure envelope: %v", err)
	}
	if withCurrent.Error.Current != string(edited.Comment.Rev) {
		t.Errorf("current = %q, want the rev the file holds now (%q)",
			withCurrent.Error.Current, edited.Comment.Rev)
	}

	// The refused write really wrote nothing.
	current := decode[commentUpdateResult](t, call(t, v, "comment.update", map[string]any{
		"path": comment.Path, "rev": "*",
		"setExternal": []map[string]any{{"system": "youtrack", "id": "4-90"}},
	}))
	if current.Comment.Body != "Second draft." {
		t.Errorf("body = %q, want the text of the writer who won", current.Comment.Body)
	}
}

// TestCommentUpdateStampsUpdatedOnlyForAnEdit records a decision a reader would
// not guess: recording an external reference is bookkeeping, not an edit, so it
// must not move the `updated` stamp a reader sees next to the comment. Changing
// the text does move it.
func TestCommentUpdateStampsUpdatedOnlyForAnEdit(t *testing.T) {
	v, _ := loadedVault(t)
	comment := seedComment(t, v, "Pushed upstream.")

	bookkeeping := decode[commentUpdateResult](t, call(t, v, "comment.update", map[string]any{
		"path": comment.Path, "rev": string(comment.Rev),
		"setExternal": []map[string]any{{"system": "youtrack", "id": "4-90"}},
	}))
	if !bookkeeping.Comment.Updated.IsZero() {
		t.Errorf("updated = %v, want it untouched: recording a remote id is not an edit",
			bookkeeping.Comment.Updated)
	}

	edit := decode[commentUpdateResult](t, call(t, v, "comment.update", map[string]any{
		"path": comment.Path, "rev": string(bookkeeping.Comment.Rev), "body": "Rephrased.",
	}))
	if edit.Comment.Updated.IsZero() {
		t.Error("updated is empty after the text changed")
	}
	if len(edit.Comment.External) != 1 {
		t.Errorf("external = %+v, want the reference to survive a text edit", edit.Comment.External)
	}
}

// TestCommentUpdateRejectsBadRequests covers the refusals that happen before
// anything is written.
func TestCommentUpdateRejectsBadRequests(t *testing.T) {
	v, _ := loadedVault(t)
	comment := seedComment(t, v, "Pushed upstream.")

	cases := []struct {
		name   string
		params map[string]any
		code   string
	}{
		{
			name:   "no path",
			params: map[string]any{"rev": "*", "body": "text"},
			code:   "invalid_request",
		},
		{
			name:   "no rev",
			params: map[string]any{"path": comment.Path, "body": "text"},
			code:   "invalid_request",
		},
		{
			name:   "nothing to change",
			params: map[string]any{"path": comment.Path, "rev": "*"},
			code:   "invalid_request",
		},
		{
			name:   "an empty body would blank the comment",
			params: map[string]any{"path": comment.Path, "rev": "*", "body": "   "},
			code:   "invalid_request",
		},
		{
			name: "the comment belongs to another item",
			params: map[string]any{
				"path": comment.Path, "rev": "*", "id": "DEMO-US-0002", "body": "text",
			},
			code: "invalid_request",
		},
		{
			name: "no such file",
			params: map[string]any{
				"path": "docs/.pmngr/comments/DEMO-US-0001/20260101T000000Z-nobody.md",
				"rev":  "*", "body": "text",
			},
			code: "not_found",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := rawCall(t, v, "comment.update", tc.params)
			if env.OK {
				t.Fatalf("%v was accepted", tc.params)
			}
			if env.Error.Code != tc.code {
				t.Fatalf("code = %q, want %q (%s)", env.Error.Code, tc.code, env.Error.Message)
			}
		})
	}

	// Nothing above touched the file.
	thread := decode[[]core.Comment](t, call(t, v, "comment.list", map[string]any{"id": "DEMO-US-0001"}))
	for _, c := range thread {
		if c.Path == comment.Path && c.Body != "Pushed upstream." {
			t.Fatalf("the comment was changed by a refused call: %q", c.Body)
		}
	}
}

// TestCommentUpdateReachesTheFileSystem asserts the write lands on disk, so a
// caller that reads the file afterwards — git, the watcher, another process —
// sees what the vault answered.
func TestCommentUpdateReachesTheFileSystem(t *testing.T) {
	v, root := diskVault(t)
	comment := seedComment(t, v, "Pushed upstream.")

	updated := decode[commentUpdateResult](t, call(t, v, "comment.update", map[string]any{
		"path": comment.Path, "rev": string(comment.Rev), "body": "Pushed and edited.",
		"setExternal": []map[string]any{{"system": "youtrack", "id": "4-90"}},
	}))
	if updated.Comment.Body != "Pushed and edited." {
		t.Fatalf("body = %q", updated.Comment.Body)
	}

	raw := onDisk(t, root, comment.Path)
	parsed, err := core.ParseComment(comment.Path, []byte(raw))
	if err != nil {
		t.Fatalf("the file on disk does not parse: %v", err)
	}
	if parsed.Body != "Pushed and edited." {
		t.Errorf("body on disk = %q", parsed.Body)
	}
	if len(parsed.External) != 1 || parsed.External[0].ID != "4-90" {
		t.Errorf("external on disk = %+v", parsed.External)
	}
	if core.ComputeRev([]byte(raw)) != updated.Comment.Rev {
		t.Errorf("the rev answered is not the rev of the bytes on disk")
	}
}

// mustJSON encodes params for a raw Call.
func mustJSON(t *testing.T, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	return string(data)
}
