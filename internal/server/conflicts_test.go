package server

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The conflict resolver over the HTTP API (GIT-US-0022, AC 9) and the Phase 4
// exit criterion of docs/11-roadmap.md, milestone GIT-M-0005: "two clones of
// the same project, edited concurrently, are reconciled through the UI without
// touching a terminal, including one real conflict".
//
// Every case drives the two clones of newSyncServer into a real conflict and
// then does exactly what the UI does: run the sync, read the conflicted file,
// resolve it, and let the resolution finish the rebase and publish the result.

// storyPath is the fixture story both sides edit.
const storyPath = "docs/.pmngr/stories/DEMO-US-0001-guest-checkout.md"

// conflictFileBody is the documented shape of GET /api/v1/sync/conflicts/file.
type conflictFileBody struct {
	Repo      string `json:"repo"`
	Path      string `json:"path"`
	Kind      string `json:"kind"`
	Operation string `json:"operation"`
	Versions  struct {
		Base      string `json:"base"`
		Ours      string `json:"ours"`
		Theirs    string `json:"theirs"`
		Working   string `json:"working"`
		HasBase   bool   `json:"hasBase"`
		HasOurs   bool   `json:"hasOurs"`
		HasTheirs bool   `json:"hasTheirs"`
		Binary    bool   `json:"binary"`
	} `json:"versions"`
	Merge *struct {
		Structured bool `json:"structured"`
		Fields     []struct {
			Field  string `json:"field"`
			Kind   string `json:"kind"`
			Choice string `json:"choice"`
			Review bool   `json:"review"`
			Merged any    `json:"merged"`
		} `json:"fields"`
		Hunks []struct {
			Index      int    `json:"index"`
			Section    string `json:"section"`
			Ours       string `json:"ours"`
			Theirs     string `json:"theirs"`
			Conflicted bool   `json:"conflicted"`
			Suggestion string `json:"suggestion"`
		} `json:"hunks"`
		Content    string `json:"content"`
		Conflicted int    `json:"conflicted"`
		Review     int    `json:"review"`
		Clean      bool   `json:"clean"`
	} `json:"merge"`
}

// conflictResolveBody is the documented shape of POST /sync/conflicts/resolve.
type conflictResolveBody struct {
	Repo  string `json:"repo"`
	Path  string `json:"path"`
	Merge struct {
		Content string `json:"content"`
		Clean   bool   `json:"clean"`
	} `json:"merge"`
	Result struct {
		Staged    bool `json:"staged"`
		Continued bool `json:"continued"`
		Remaining []struct {
			Path string `json:"path"`
		} `json:"remaining"`
		Status struct {
			State     string `json:"state"`
			Operation string `json:"operation"`
			Ahead     int    `json:"ahead"`
		} `json:"status"`
	} `json:"result"`
}

// editStory rewrites one line of the fixture story in a clone.
func editStory(t *testing.T, dir, from, to string) string {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(storyPath))
	raw, err := os.ReadFile(full)
	if err != nil {
		t.Fatalf("read the fixture story: %v", err)
	}
	text := strings.Replace(string(raw), from, to, 1)
	if text == string(raw) {
		t.Fatalf("the fixture story does not contain %q", from)
	}
	if err := os.WriteFile(full, []byte(text), 0o644); err != nil {
		t.Fatalf("write the fixture story: %v", err)
	}
	return text
}

// conflictedFixture drives both clones into a real conflict on the same story
// and returns the fixture with the rebase stopped.
func conflictedFixture(t *testing.T) syncFixture {
	t.Helper()
	fx := newSyncServer(t, syncGitSettings())

	// The teammate moves the story on and adds a label, then publishes.
	editStory(t, fx.peer, "status: in_progress", "status: done")
	editStory(t, fx.peer, "labels: [frontend]", "labels: [frontend, checkout]")
	editStory(t, fx.peer, "updated: 2026-09-01T10:45:12Z", "updated: 2026-09-03T10:00:00Z")
	editStory(t, fx.peer, "- [ ] Address validation errors are shown inline, field by field.",
		"- [x] Address validation errors are shown inline, field by field.")
	runGit(t, fx.peer, "add", storyPath)
	runGit(t, fx.peer, "commit", "-m", "docs: the teammate closes the story")
	runGit(t, fx.peer, "push", "origin", "main")

	// We edit the same story differently, offline.
	editStory(t, fx.local, "assignees: [marta, jose]", "assignees: [marta, jose, ana]")
	editStory(t, fx.local, "updated: 2026-09-01T10:45:12Z", "updated: 2026-09-02T09:00:00Z")
	editStory(t, fx.local, "As a shopper without an account,", "As a guest shopper,")
	runGit(t, fx.local, "add", storyPath)
	runGit(t, fx.local, "commit", "-m", "docs: my own wording")

	var run syncRunBody
	decode(t, send(t, fx.server, request{
		method: http.MethodPost, target: "/api/v1/sync/run", body: map[string]any{},
	}), http.StatusOK, &run)
	res := run.Results[0]
	if res.Phase != "conflicts" || res.Code != "git_conflict" {
		t.Fatalf("want a conflict, got phase=%q code=%q message=%q", res.Phase, res.Code, res.Message)
	}
	if len(res.Conflicts) != 1 || res.Conflicts[0].Path != storyPath {
		t.Fatalf("conflicts = %+v", res.Conflicts)
	}
	return fx
}

// readConflict fetches the structured view of the conflicted story.
func readConflict(t *testing.T, fx syncFixture) conflictFileBody {
	t.Helper()
	var body conflictFileBody
	decode(t, send(t, fx.server, request{
		method: http.MethodGet,
		target: "/api/v1/sync/conflicts/file?repo=" + testRepoID + "&path=" + storyPath,
	}), http.StatusOK, &body)
	return body
}

func TestSyncConflictFileSurface(t *testing.T) {
	t.Parallel()

	fx := conflictedFixture(t)
	body := readConflict(t, fx)

	t.Run("the three versions come back", func(t *testing.T) {
		if !body.Versions.HasBase || !body.Versions.HasOurs || !body.Versions.HasTheirs {
			t.Fatalf("versions = %+v", body.Versions)
		}
		if body.Versions.Binary {
			t.Errorf("a Markdown story was reported as binary")
		}
		if body.Operation != "rebase" {
			t.Errorf("operation = %q, want rebase", body.Operation)
		}
	})

	t.Run("the front matter is merged field by field, not as text", func(t *testing.T) {
		if body.Merge == nil || !body.Merge.Structured {
			t.Fatalf("merge = %+v", body.Merge)
		}
		if strings.Contains(body.Merge.Content, "<<<<<<<") {
			t.Fatalf("the merged file carries conflict markers:\n%s", body.Merge.Content)
		}
		// Both sides' list additions survive: nobody's label or assignee is lost.
		for _, want := range []string{"ana", "checkout", "frontend", "marta"} {
			if !strings.Contains(body.Merge.Content, want) {
				t.Errorf("the merge lost %q:\n%s", want, body.Merge.Content)
			}
		}
		// The status only the teammate changed is taken, and reported.
		if !strings.Contains(body.Merge.Content, "status: done") {
			t.Errorf("the remote status was not taken:\n%s", body.Merge.Content)
		}
		if len(body.Merge.Fields) == 0 {
			t.Fatal("no field decision was reported, so nothing could be overridden")
		}
	})

	t.Run("the body hunk is reported with its section and both sides", func(t *testing.T) {
		if len(body.Merge.Hunks) == 0 {
			t.Fatal("no body hunk was reported")
		}
		// The checkbox one side ticked stays ticked, automatically.
		if !strings.Contains(body.Merge.Content, "- [x] Address validation errors") {
			t.Errorf("a ticked criterion was un-ticked by the merge:\n%s", body.Merge.Content)
		}
		if !strings.Contains(body.Merge.Content, "As a guest shopper,") {
			t.Errorf("the local wording, which only we changed, was lost:\n%s", body.Merge.Content)
		}
	})

	t.Run("a path that is not conflicted is a 404", func(t *testing.T) {
		decode(t, send(t, fx.server, request{
			method: http.MethodGet,
			target: "/api/v1/sync/conflicts/file?repo=" + testRepoID + "&path=docs/index.md",
		}), http.StatusNotFound, nil)
	})
}

func TestSyncConflictResolve(t *testing.T) {
	t.Parallel()

	t.Run("two clones are reconciled end to end through the API", func(t *testing.T) {
		t.Parallel()
		fx := conflictedFixture(t)
		analysis := readConflict(t, fx)
		if analysis.Merge == nil {
			t.Fatal("no merge was proposed")
		}

		var resolved conflictResolveBody
		decode(t, send(t, fx.server, request{
			method: http.MethodPost, target: "/api/v1/sync/conflicts/resolve",
			body: map[string]any{
				"repo": testRepoID, "path": storyPath, "resolution": "merged",
			},
		}), http.StatusOK, &resolved)

		if !resolved.Result.Staged || !resolved.Result.Continued {
			t.Fatalf("result = %+v", resolved.Result)
		}
		if len(resolved.Result.Remaining) != 0 {
			t.Fatalf("remaining = %+v", resolved.Result.Remaining)
		}
		if resolved.Result.Status.Operation != "" || resolved.Result.Status.State == "conflicted" {
			t.Fatalf("the rebase was not finished: %+v", resolved.Result.Status)
		}

		// The file on disk is the canonical merge: no markers, both sides' work.
		raw, err := os.ReadFile(filepath.Join(fx.local, filepath.FromSlash(storyPath)))
		if err != nil {
			t.Fatalf("read the resolved story: %v", err)
		}
		text := string(raw)
		if strings.Contains(text, "<<<<<<<") {
			t.Fatalf("the resolved file carries conflict markers:\n%s", text)
		}
		for _, want := range []string{"status: done", "ana", "checkout", "As a guest shopper,"} {
			if !strings.Contains(text, want) {
				t.Errorf("the resolved story lost %q:\n%s", want, text)
			}
		}

		// It validates: the core parses it back as the same item.
		var validation []struct {
			Code     string `json:"code"`
			Severity string `json:"severity"`
		}
		decode(t, send(t, fx.server, request{
			method: http.MethodPost, target: "/api/v1/validate",
			body: map[string]any{"text": text, "path": storyPath},
		}), http.StatusOK, &validation)
		for _, d := range validation {
			if d.Severity == "error" {
				t.Errorf("the resolved file does not validate: %+v", validation)
				break
			}
		}

		// And the second sync publishes it, which the teammate then sees.
		var run syncRunBody
		decode(t, send(t, fx.server, request{
			method: http.MethodPost, target: "/api/v1/sync/run", body: map[string]any{},
		}), http.StatusOK, &run)
		if run.Results[0].Phase != "done" {
			t.Fatalf("the follow-up sync did not finish: %+v", run.Results[0])
		}
		runGit(t, fx.peer, "pull", "--ff-only", "origin", "main")
		peerRaw, err := os.ReadFile(filepath.Join(fx.peer, filepath.FromSlash(storyPath)))
		if err != nil {
			t.Fatalf("read the story in the teammate's clone: %v", err)
		}
		if !strings.Contains(string(peerRaw), "As a guest shopper,") {
			t.Fatalf("the teammate did not receive the resolution:\n%s", peerRaw)
		}
	})

	t.Run("keep mine writes our side verbatim and finishes the rebase", func(t *testing.T) {
		t.Parallel()
		fx := conflictedFixture(t)
		analysis := readConflict(t, fx)

		var resolved conflictResolveBody
		decode(t, send(t, fx.server, request{
			method: http.MethodPost, target: "/api/v1/sync/conflicts/resolve",
			body: map[string]any{"repo": testRepoID, "path": storyPath, "resolution": "ours"},
		}), http.StatusOK, &resolved)

		if !resolved.Result.Continued {
			t.Fatalf("result = %+v", resolved.Result)
		}
		if resolved.Merge.Content != analysis.Versions.Ours {
			t.Errorf("keep-mine did not write our version verbatim")
		}
	})

	t.Run("keep theirs writes the remote side verbatim", func(t *testing.T) {
		t.Parallel()
		fx := conflictedFixture(t)
		analysis := readConflict(t, fx)

		var resolved conflictResolveBody
		decode(t, send(t, fx.server, request{
			method: http.MethodPost, target: "/api/v1/sync/conflicts/resolve",
			body: map[string]any{"repo": testRepoID, "path": storyPath, "resolution": "theirs"},
		}), http.StatusOK, &resolved)
		if resolved.Merge.Content != analysis.Versions.Theirs {
			t.Errorf("keep-theirs did not write the remote version verbatim")
		}
	})

	t.Run("a manual edit is written as given", func(t *testing.T) {
		t.Parallel()
		fx := conflictedFixture(t)
		analysis := readConflict(t, fx)
		edited := strings.Replace(analysis.Merge.Content, "As a guest shopper,", "As any shopper,", 1)

		var resolved conflictResolveBody
		decode(t, send(t, fx.server, request{
			method: http.MethodPost, target: "/api/v1/sync/conflicts/resolve",
			body: map[string]any{
				"repo": testRepoID, "path": storyPath,
				"resolution": "manual", "content": edited,
			},
		}), http.StatusOK, &resolved)
		if !strings.Contains(resolved.Merge.Content, "As any shopper,") {
			t.Errorf("the manual edit was not written:\n%s", resolved.Merge.Content)
		}
	})

	t.Run("a per-field override flips an automatic decision", func(t *testing.T) {
		t.Parallel()
		fx := conflictedFixture(t)

		var resolved conflictResolveBody
		decode(t, send(t, fx.server, request{
			method: http.MethodPost, target: "/api/v1/sync/conflicts/resolve",
			body: map[string]any{
				"repo": testRepoID, "path": storyPath, "resolution": "merged",
				"fields": map[string]string{"status": "ours"},
			},
		}), http.StatusOK, &resolved)
		if !strings.Contains(resolved.Merge.Content, "status: in_progress") {
			t.Errorf("the field override was ignored:\n%s", resolved.Merge.Content)
		}
	})

	t.Run("resolving without continuing leaves the rebase abortable", func(t *testing.T) {
		t.Parallel()
		fx := conflictedFixture(t)
		proceed := false

		var resolved conflictResolveBody
		decode(t, send(t, fx.server, request{
			method: http.MethodPost, target: "/api/v1/sync/conflicts/resolve",
			body: map[string]any{
				"repo": testRepoID, "path": storyPath, "resolution": "merged",
				"continue": proceed,
			},
		}), http.StatusOK, &resolved)
		if resolved.Result.Continued {
			t.Fatal("the rebase was continued without being asked")
		}
		if resolved.Result.Status.Operation != "rebase" {
			t.Fatalf("status = %+v", resolved.Result.Status)
		}

		// Abort is still available, and restores the pre-sync state exactly.
		decode(t, send(t, fx.server, request{
			method: http.MethodPost, target: "/api/v1/sync/abort",
			body: map[string]any{"repo": testRepoID},
		}), http.StatusOK, nil)
		raw, err := os.ReadFile(filepath.Join(fx.local, filepath.FromSlash(storyPath)))
		if err != nil {
			t.Fatalf("read the story after the abort: %v", err)
		}
		if !strings.Contains(string(raw), "status: in_progress") ||
			!strings.Contains(string(raw), "labels: [frontend]") {
			t.Fatalf("the abort did not restore our version:\n%s", raw)
		}
	})

	t.Run("a resolution for an unknown repository is refused", func(t *testing.T) {
		t.Parallel()
		fx := conflictedFixture(t)
		decode(t, send(t, fx.server, request{
			method: http.MethodPost, target: "/api/v1/sync/conflicts/resolve",
			body: map[string]any{"repo": "nope", "path": storyPath, "resolution": "ours"},
		}), http.StatusNotFound, nil)
	})

	t.Run("an unknown resolution is refused before anything is written", func(t *testing.T) {
		t.Parallel()
		fx := conflictedFixture(t)
		decode(t, send(t, fx.server, request{
			method: http.MethodPost, target: "/api/v1/sync/conflicts/resolve",
			body: map[string]any{"repo": testRepoID, "path": storyPath, "resolution": "octopus"},
		}), http.StatusBadRequest, nil)
	})

	t.Run("a manual resolution with no content is refused", func(t *testing.T) {
		t.Parallel()
		fx := conflictedFixture(t)
		decode(t, send(t, fx.server, request{
			method: http.MethodPost, target: "/api/v1/sync/conflicts/resolve",
			body: map[string]any{"repo": testRepoID, "path": storyPath, "resolution": "manual"},
		}), http.StatusBadRequest, nil)
	})
}
