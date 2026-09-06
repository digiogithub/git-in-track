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
	"github.com/digiogithub/git-in-track/internal/core"
	"github.com/digiogithub/git-in-track/internal/gitops"
)

// The Jujutsu surface: what the API says about a jj repository (GIT-US-0038)
// and what it now does to one through jj (GIT-US-0041).

// initJujutsuRepo turns a directory into a colocated jj repository with one
// bookmarked commit, skipping the test when no jj binary is installed.
func initJujutsuRepo(t *testing.T, dir string) {
	t.Helper()
	bin, _, err := gitops.ResolveJujutsu("")
	if err != nil {
		t.Skipf("these tests need a jj binary: %v", err)
	}
	jjConfig := filepath.Join(t.TempDir(), "jj-config.toml")
	if writeErr := os.WriteFile(jjConfig,
		[]byte("[user]\nname = \"Test User\"\nemail = \"test@example.com\"\n"), 0o600); writeErr != nil {
		t.Fatalf("write the jj configuration: %v", writeErr)
	}
	run := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), bin, args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"JJ_CONFIG="+jjConfig,
			"JJ_USER=Test User",
			"JJ_EMAIL=test@example.com",
			"GIT_TERMINAL_PROMPT=0",
		)
		if out, cmdErr := cmd.CombinedOutput(); cmdErr != nil {
			t.Fatalf("jj %s: %v\n%s", strings.Join(args, " "), cmdErr, out)
		}
	}
	run("git", "init", "--colocate")
	run("describe", "-m", "chore: seed the fixture")
	run("bookmark", "create", "main", "-r", "@")
	run("new")
}

// newJujutsuServer mounts the fixture as a colocated jj repository.
func newJujutsuServer(t *testing.T, git config.Git) (*Server, string) {
	t.Helper()
	root := copyTree(t, fixtureRoot)
	initJujutsuRepo(t, root)
	s, err := New(Options{
		Token:     "test-token",
		Version:   "0.0.1-test",
		Workspace: "test",
		Repos:     []Repo{{ID: testRepoID, Path: root, Role: "project", DocsFolder: "docs"}},
		Now:       func() time.Time { return time.Date(2026, 9, 6, 10, 0, 0, 0, time.UTC) },
		Git:       git,
	})
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	t.Cleanup(func() { s.git.close(t.Context()) })
	return s, root
}

// TestJujutsuRepositorySurface checks that the API describes a jj repository as
// jj — never as a detached, permanently dirty git one — and writes to it
// through jj rather than refusing it.
func TestJujutsuRepositorySurface(t *testing.T) {
	// Since GIT-US-0040 the state is the truthful one — this fixture has a
	// bookmark and no remote, so it is `no_remote` — and the `jujutsu` flag is
	// what tells the UI to render the repository as jj-managed. The `jujutsu`
	// state itself is now the degraded reading of a repository whose jj binary
	// is missing.
	t.Run("the sync status reports the kind and a truthful state", func(t *testing.T) {
		s, _ := newJujutsuServer(t, config.Default().Git)
		var body struct {
			Repos []syncRepoStatus `json:"repos"`
		}
		decode(t, send(t, s, request{method: http.MethodGet, target: "/api/v1/sync/status"}),
			http.StatusOK, &body)
		if len(body.Repos) != 1 {
			t.Fatalf("repos = %+v", body.Repos)
		}
		repo := body.Repos[0]
		if !repo.VCS.IsJujutsu() || repo.VCS.Layout != core.LayoutColocated {
			t.Fatalf("vcs = %+v, want a colocated jj repository", repo.VCS)
		}
		if repo.Status == nil {
			t.Fatalf("no status was read: %s", repo.Reason)
		}
		if repo.Status.State != gitops.StateNoRemote {
			t.Errorf("state = %q, want %q", repo.Status.State, gitops.StateNoRemote)
		}
		if repo.Status.Anonymous {
			t.Error("a jj working copy is reported as a detached HEAD")
		}
		if !repo.Status.Jujutsu {
			t.Error("the status does not carry the jujutsu flag")
		}
		if !repo.Writes {
			t.Error("a jj repository driven by jj is reported as read-only")
		}
	})

	t.Run("commit on save records a jj commit when its batch is flushed", func(t *testing.T) {
		settings := config.Default().Git
		settings.CommitOnSave = true
		settings.CommitDebounce = time.Hour // the batch stays pending until it is flushed
		s, _ := newJujutsuServer(t, settings)
		createFixtureItem(t, s, "Written while jj owns the repository")

		var body gitCommitBody
		decode(t, send(t, s, request{method: http.MethodPost, target: "/api/v1/git/commit"}),
			http.StatusOK, &body)
		if len(body.Commits) != 1 {
			t.Fatalf("commits = %+v, want the flushed batch", body.Commits)
		}
		if body.Commits[0].Code != "" {
			t.Fatalf("commit on save failed: %+v", body.Commits[0])
		}
		if body.Commits[0].SHA == "" {
			t.Errorf("no commit was recorded in the jj repository: %+v", body.Commits[0])
		}
	})

	t.Run("an explicit commit records exactly the requested paths", func(t *testing.T) {
		s, root := newJujutsuServer(t, config.Default().Git)
		if err := os.WriteFile(filepath.Join(root, "docs", "note.md"),
			[]byte("# a note\n"), 0o600); err != nil {
			t.Fatalf("write the file to commit: %v", err)
		}

		var body gitCommitBody
		decode(t, send(t, s, request{
			method: http.MethodPost,
			target: "/api/v1/git/commit",
			body: map[string]any{
				"repo":    testRepoID,
				"paths":   []string{"docs/note.md"},
				"message": "docs: an explicit commit",
			},
		}), http.StatusOK, &body)
		if len(body.Commits) != 1 || body.Commits[0].SHA == "" {
			t.Fatalf("commits = %+v, want one recorded commit", body.Commits)
		}
		if body.Commits[0].Subject != "docs: an explicit commit" {
			t.Errorf("subject = %q, want the message that was asked for", body.Commits[0].Subject)
		}
	})

	t.Run("a sync run is no longer refused for being jj", func(t *testing.T) {
		s, _ := newJujutsuServer(t, config.Default().Git)
		var body syncRunResponse
		decode(t, send(t, s, request{
			method: http.MethodPost,
			target: "/api/v1/sync/run",
			body:   map[string]any{"dryRun": true},
		}), http.StatusOK, &body)
		if len(body.Results) != 1 {
			t.Fatalf("results = %+v", body.Results)
		}
		res := body.Results[0]
		// The fixture has a bookmark and no remote, so the run stops for the
		// one honest reason there is — and says so in jj's own vocabulary.
		if res.Code != gitops.CodeNoRemote {
			t.Errorf("code = %q, want %q (%s)", res.Code, gitops.CodeNoRemote, res.Message)
		}
		if !strings.Contains(res.Message, "jj git remote add") {
			t.Errorf("the message does not speak jj: %s", res.Message)
		}
		if strings.Contains(res.Message, "check out the branch") {
			t.Errorf("the message still gives git advice: %s", res.Message)
		}
	})
}
