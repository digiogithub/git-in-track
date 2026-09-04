package server

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/digiogithub/git-in-track/internal/config"
)

// The sync surface over two clones of one repository (GIT-US-0021, AC 9).
//
// The fixture is the real thing: a bare repository in a temporary directory and
// two clones of it. The server is mounted on the first clone, the second stands
// in for a teammate, and the reconciliation happens through the HTTP API.

// syncStatusBody is the documented shape of GET /api/v1/sync/status.
type syncStatusBody struct {
	Repos []struct {
		Repo    string `json:"repo"`
		Git     bool   `json:"git"`
		Reason  string `json:"reason"`
		Backend string `json:"backend"`
		Pending int    `json:"pending"`
		Status  *struct {
			Branch     string   `json:"branch"`
			Clean      bool     `json:"clean"`
			Dirty      []string `json:"dirty"`
			Remote     string   `json:"remote"`
			RemoteURL  string   `json:"remoteUrl"`
			Upstream   string   `json:"upstream"`
			Ahead      int      `json:"ahead"`
			Behind     int      `json:"behind"`
			State      string   `json:"state"`
			Operation  string   `json:"operation"`
			Conflicted []struct {
				Path string `json:"path"`
				Kind string `json:"kind"`
			} `json:"conflicted"`
		} `json:"status"`
	} `json:"repos"`
	Settings struct {
		PullStrategy   string `json:"pullStrategy"`
		PushOnSync     bool   `json:"pushOnSync"`
		MaxPushRetries int    `json:"maxPushRetries"`
		Supported      bool   `json:"supported"`
	} `json:"settings"`
}

// syncRunBody is the documented shape of POST /api/v1/sync/run.
type syncRunBody struct {
	OperationID string `json:"operationId"`
	DryRun      bool   `json:"dryRun"`
	Results     []struct {
		Repo     string `json:"repo"`
		Phase    string `json:"phase"`
		Strategy string `json:"strategy"`
		Pulled   int    `json:"pulled"`
		Pushed   int    `json:"pushed"`
		Code     string `json:"code"`
		Message  string `json:"message"`
		Incoming []struct {
			Subject string `json:"subject"`
		} `json:"incoming"`
		Outgoing []struct {
			Subject string `json:"subject"`
		} `json:"outgoing"`
		Conflicts []struct {
			Path string `json:"path"`
		} `json:"conflicts"`
	} `json:"results"`
	Commits []struct {
		Repo string `json:"repo"`
		SHA  string `json:"sha"`
	} `json:"commits"`
}

// syncFixture is a bare remote plus the clone the server serves and the clone
// that stands in for a teammate.
type syncFixture struct {
	server *Server
	local  string
	peer   string
}

// runGit runs git in dir and fails the test on error.
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	bin, err := exec.LookPath("git")
	if err != nil {
		t.Skipf("these tests need a git binary: %v", err)
	}
	cmd := exec.CommandContext(t.Context(), bin, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL="+filepath.Join(t.TempDir(), "absent"),
		"GIT_CONFIG_SYSTEM="+filepath.Join(t.TempDir(), "absent"),
		"GIT_TERMINAL_PROMPT=0",
	)
	if out, cmdErr := cmd.CombinedOutput(); cmdErr != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), cmdErr, out)
	}
}

// identify gives a clone the identity its commits need.
func identify(t *testing.T, dir string) {
	t.Helper()
	runGit(t, dir, "config", "user.name", "Test User")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "commit.gpgsign", "false")
}

// newSyncServer builds the two clones and mounts the first one.
func newSyncServer(t *testing.T, git config.Git) syncFixture {
	t.Helper()
	seed := copyTree(t, fixtureRoot)
	initGitRepo(t, seed)

	root := t.TempDir()
	remote := filepath.Join(root, "remote.git")
	runGit(t, root, "clone", "--bare", seed, remote)
	local := filepath.Join(root, "local")
	peer := filepath.Join(root, "peer")
	runGit(t, root, "clone", remote, local)
	runGit(t, root, "clone", remote, peer)
	identify(t, local)
	identify(t, peer)

	s, err := New(Options{
		Token:     "test-token",
		Version:   "0.0.1-test",
		Workspace: "test",
		Repos:     []Repo{{ID: testRepoID, Path: local, Role: "project", DocsFolder: "docs"}},
		Now:       func() time.Time { return time.Date(2026, 9, 4, 10, 0, 0, 0, time.UTC) },
		Git:       git,
	})
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	t.Cleanup(func() { s.git.close(t.Context()) })
	return syncFixture{server: s, local: local, peer: peer}
}

// peerCommit commits a file in the teammate's clone and pushes it.
func peerCommit(t *testing.T, dir, rel, text, subject string) {
	t.Helper()
	target := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(target, []byte(text), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	runGit(t, dir, "add", rel)
	runGit(t, dir, "commit", "-m", subject)
	runGit(t, dir, "push", "origin", "main")
}

// syncSettings returns the shipped git settings with commit-on-save on and no
// debounce, so a write is committed inline and the test needs no sleep.
func syncGitSettings() config.Git {
	git := config.Default().Git
	git.CommitOnSave = true
	git.CommitDebounce = -1
	return git
}

func TestSyncStatusSurface(t *testing.T) {
	t.Parallel()

	fx := newSyncServer(t, config.Default().Git)
	var body syncStatusBody
	decode(t, send(t, fx.server, request{method: http.MethodGet, target: "/api/v1/sync/status"}),
		http.StatusOK, &body)

	if len(body.Repos) != 1 || !body.Repos[0].Git {
		t.Fatalf("repos = %+v", body.Repos)
	}
	st := body.Repos[0].Status
	if st == nil {
		t.Fatalf("no status: %+v", body.Repos[0])
	}
	if st.Branch != "main" || st.Remote != "origin" || st.Upstream != "origin/main" {
		t.Fatalf("branch/remote/upstream = %q/%q/%q", st.Branch, st.Remote, st.Upstream)
	}
	if st.State != "up_to_date" || st.Ahead != 0 || st.Behind != 0 {
		t.Fatalf("state=%q ahead=%d behind=%d", st.State, st.Ahead, st.Behind)
	}
	if body.Settings.PullStrategy != "rebase" || !body.Settings.Supported {
		t.Fatalf("settings = %+v", body.Settings)
	}
}

func TestSyncRun(t *testing.T) {
	t.Parallel()

	t.Run("a dry run previews the incoming work and changes nothing", func(t *testing.T) {
		t.Parallel()
		fx := newSyncServer(t, config.Default().Git)
		peerCommit(t, fx.peer, "docs/incoming.md", "incoming\n", "docs: teammate work")

		var body syncRunBody
		decode(t, send(t, fx.server, request{
			method: http.MethodPost, target: "/api/v1/sync/run",
			body: map[string]any{"dryRun": true},
		}), http.StatusOK, &body)

		if len(body.Results) != 1 {
			t.Fatalf("results = %+v", body.Results)
		}
		res := body.Results[0]
		if res.Phase != "done" || res.Pulled != 0 || res.Pushed != 0 {
			t.Fatalf("phase=%q pulled=%d pushed=%d", res.Phase, res.Pulled, res.Pushed)
		}
		if len(res.Incoming) != 1 || res.Incoming[0].Subject != "docs: teammate work" {
			t.Fatalf("incoming = %+v", res.Incoming)
		}
		if _, err := os.Stat(filepath.Join(fx.local, "docs", "incoming.md")); err == nil {
			t.Fatal("the dry run wrote a file into the working tree")
		}
	})

	t.Run("a run brings the teammate's work in", func(t *testing.T) {
		t.Parallel()
		fx := newSyncServer(t, config.Default().Git)
		peerCommit(t, fx.peer, "docs/incoming.md", "incoming\n", "docs: teammate work")

		var body syncRunBody
		decode(t, send(t, fx.server, request{
			method: http.MethodPost, target: "/api/v1/sync/run", body: map[string]any{},
		}), http.StatusOK, &body)

		res := body.Results[0]
		if res.Phase != "done" || res.Pulled != 1 {
			t.Fatalf("phase=%q pulled=%d code=%q message=%q", res.Phase, res.Pulled, res.Code, res.Message)
		}
		if _, err := os.Stat(filepath.Join(fx.local, "docs", "incoming.md")); err != nil {
			t.Fatalf("the incoming file did not land: %v", err)
		}
		if body.OperationID == "" {
			t.Error("a run must carry an operation id")
		}
	})

	t.Run("an edit is committed by the preflight and published to the teammate", func(t *testing.T) {
		t.Parallel()
		fx := newSyncServer(t, syncGitSettings())

		// Create an item through the API: commit-on-save commits it locally.
		id := createFixtureItem(t, fx.server, "Synced from the companion")
		fx.server.git.flush(t.Context())

		var body syncRunBody
		decode(t, send(t, fx.server, request{
			method: http.MethodPost, target: "/api/v1/sync/run", body: map[string]any{},
		}), http.StatusOK, &body)

		res := body.Results[0]
		if res.Phase != "done" || res.Pushed != 1 {
			t.Fatalf("phase=%q pushed=%d code=%q message=%q", res.Phase, res.Pushed, res.Code, res.Message)
		}
		// The teammate sees it after a pull, which is the whole point.
		runGit(t, fx.peer, "pull", "--ff-only", "origin", "main")
		subjects := gitLog(t, fx.peer)
		if len(subjects) == 0 || !strings.Contains(subjects[0], id) {
			t.Fatalf("the teammate's log does not carry the edit: %v", subjects)
		}
	})

	t.Run("diverged clones are rebased and published in one run", func(t *testing.T) {
		t.Parallel()
		fx := newSyncServer(t, syncGitSettings())
		peerCommit(t, fx.peer, "docs/theirs.md", "theirs\n", "docs: teammate work")

		createFixtureItem(t, fx.server, "Mine, rebased onto theirs")
		fx.server.git.flush(t.Context())

		var body syncRunBody
		decode(t, send(t, fx.server, request{
			method: http.MethodPost, target: "/api/v1/sync/run", body: map[string]any{},
		}), http.StatusOK, &body)

		res := body.Results[0]
		if res.Phase != "done" || res.Pulled != 1 || res.Pushed != 1 {
			t.Fatalf("phase=%q pulled=%d pushed=%d code=%q message=%q",
				res.Phase, res.Pulled, res.Pushed, res.Code, res.Message)
		}
		if _, err := os.Stat(filepath.Join(fx.local, "docs", "theirs.md")); err != nil {
			t.Fatalf("the teammate's file did not land: %v", err)
		}
		runGit(t, fx.peer, "pull", "--ff-only", "origin", "main")
		if len(gitLog(t, fx.peer)) < 3 {
			t.Fatalf("the teammate did not receive the rebased commit: %v", gitLog(t, fx.peer))
		}
	})

	t.Run("an uncommitted edit refuses the run with an actionable message", func(t *testing.T) {
		t.Parallel()
		fx := newSyncServer(t, config.Default().Git)
		if err := os.WriteFile(filepath.Join(fx.local, "docs", "index.md"), []byte("# edited\n"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}

		var body syncRunBody
		decode(t, send(t, fx.server, request{
			method: http.MethodPost, target: "/api/v1/sync/run", body: map[string]any{},
		}), http.StatusOK, &body)

		res := body.Results[0]
		if res.Code != "git_dirty_tree" || res.Phase != "failed" {
			t.Fatalf("code=%q phase=%q", res.Code, res.Phase)
		}
		if !strings.Contains(res.Message, "commit") {
			t.Fatalf("the message does not say what to do: %q", res.Message)
		}
	})

	t.Run("an unknown repository is refused", func(t *testing.T) {
		t.Parallel()
		fx := newSyncServer(t, config.Default().Git)
		decode(t, send(t, fx.server, request{
			method: http.MethodPost, target: "/api/v1/sync/run",
			body: map[string]any{"repos": []string{"nope"}},
		}), http.StatusNotFound, nil)
	})

	t.Run("an unknown strategy is refused before anything happens", func(t *testing.T) {
		t.Parallel()
		fx := newSyncServer(t, config.Default().Git)
		decode(t, send(t, fx.server, request{
			method: http.MethodPost, target: "/api/v1/sync/run",
			body: map[string]any{"strategy": "octopus"},
		}), http.StatusBadRequest, nil)
	})
}

func TestSyncConflictsAndAbort(t *testing.T) {
	t.Parallel()

	fx := newSyncServer(t, syncGitSettings())
	// Both sides edit the same file, which git cannot merge on its own.
	peerCommit(t, fx.peer, "docs/same.md", "theirs\n", "docs: theirs")
	if err := os.WriteFile(filepath.Join(fx.local, "docs", "same.md"), []byte("mine\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	runGit(t, fx.local, "add", "docs/same.md")
	runGit(t, fx.local, "commit", "-m", "docs: mine")

	var body syncRunBody
	decode(t, send(t, fx.server, request{
		method: http.MethodPost, target: "/api/v1/sync/run", body: map[string]any{},
	}), http.StatusOK, &body)

	res := body.Results[0]
	if res.Phase != "conflicts" || res.Code != "git_conflict" {
		t.Fatalf("phase=%q code=%q message=%q", res.Phase, res.Code, res.Message)
	}
	if len(res.Conflicts) != 1 || res.Conflicts[0].Path != "docs/same.md" {
		t.Fatalf("conflicts = %+v", res.Conflicts)
	}

	// The conflict is listed, so the UI can show it instead of guessing.
	var conflicts struct {
		Conflicts []struct {
			Repo      string   `json:"repo"`
			Paths     []string `json:"paths"`
			Operation string   `json:"operation"`
		} `json:"conflicts"`
	}
	decode(t, send(t, fx.server, request{method: http.MethodGet, target: "/api/v1/sync/conflicts"}),
		http.StatusOK, &conflicts)
	if len(conflicts.Conflicts) != 1 || conflicts.Conflicts[0].Paths[0] != "docs/same.md" {
		t.Fatalf("conflicts = %+v", conflicts.Conflicts)
	}

	// Aborting restores the tree, which is what "recoverable" means.
	var restored syncStatusBody
	decode(t, send(t, fx.server, request{
		method: http.MethodPost, target: "/api/v1/sync/abort",
		body: map[string]any{"repo": testRepoID},
	}), http.StatusOK, nil)
	decode(t, send(t, fx.server, request{method: http.MethodGet, target: "/api/v1/sync/status"}),
		http.StatusOK, &restored)
	st := restored.Repos[0].Status
	if st == nil || st.Operation != "" || len(st.Conflicted) != 0 {
		t.Fatalf("the abort left the repository at %+v", st)
	}
	if data, err := os.ReadFile(filepath.Join(fx.local, "docs", "same.md")); err != nil ||
		strings.TrimSpace(string(data)) != "mine" {
		t.Fatalf("the local version was not restored: %q (%v)", data, err)
	}
}

func TestSyncResolveIsDeferred(t *testing.T) {
	t.Parallel()

	fx := newSyncServer(t, config.Default().Git)
	var doc problemBody
	decode(t, send(t, fx.server, request{
		method: http.MethodPost, target: "/api/v1/sync/conflicts/resolve",
		body: map[string]any{"repo": testRepoID, "path": "docs/same.md", "resolution": "ours"},
	}), http.StatusNotImplemented, &doc)
	if doc.Code != "not_implemented" {
		t.Fatalf("code = %q", doc.Code)
	}
}
