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

// The Jujutsu surface of GIT-US-0038: what the API says about a jj repository,
// and what it refuses to do to one.

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
// jj — never as a detached, permanently dirty git one — and refuses every write
// against it with the dedicated code.
func TestJujutsuRepositorySurface(t *testing.T) {
	t.Run("the sync status reports the kind and the jujutsu state", func(t *testing.T) {
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
		if repo.Status.State != gitops.StateJujutsu {
			t.Errorf("state = %q, want %q", repo.Status.State, gitops.StateJujutsu)
		}
		if repo.Status.Detached {
			t.Error("a jj working copy is reported as a detached HEAD")
		}
		if !repo.Status.Jujutsu {
			t.Error("the status does not carry the jujutsu flag")
		}
	})

	t.Run("commit on save is refused when its batch is flushed", func(t *testing.T) {
		settings := config.Default().Git
		settings.CommitOnSave = true
		settings.CommitDebounce = time.Hour // the batch stays pending until it is flushed
		s, _ := newJujutsuServer(t, settings)
		createFixtureItem(t, s, "Written while jj owns the repository")

		var body gitCommitBody
		decode(t, send(t, s, request{method: http.MethodPost, target: "/api/v1/git/commit"}),
			http.StatusOK, &body)
		if len(body.Commits) != 1 {
			t.Fatalf("commits = %+v, want the refused batch", body.Commits)
		}
		if body.Commits[0].Code != gitops.CodeJujutsuWriteRefused {
			t.Errorf("code = %q, want %q", body.Commits[0].Code, gitops.CodeJujutsuWriteRefused)
		}
		if !strings.Contains(body.Commits[0].Message, core.JujutsuCommitCommand) {
			t.Errorf("the outcome does not name the jj command: %s", body.Commits[0].Message)
		}
		if body.Commits[0].SHA != "" {
			t.Errorf("a commit was made in a jj repository: %s", body.Commits[0].SHA)
		}
	})

	t.Run("an explicit commit is refused with the jj command to run", func(t *testing.T) {
		s, _ := newJujutsuServer(t, config.Default().Git)

		var problem problemBody
		decode(t, send(t, s, request{
			method: http.MethodPost,
			target: "/api/v1/git/commit",
			body: map[string]any{
				"repo":    testRepoID,
				"paths":   []string{"docs/.pmngr/project.yaml"},
				"message": "docs: an explicit commit",
			},
		}), http.StatusConflict, &problem)
		if problem.Code != gitops.CodeJujutsuWriteRefused {
			t.Errorf("code = %q, want %q", problem.Code, gitops.CodeJujutsuWriteRefused)
		}
		if !strings.Contains(problem.Detail, core.JujutsuCommitCommand) {
			t.Errorf("the problem does not name the jj command: %s", problem.Detail)
		}
	})

	t.Run("a sync run is refused instead of advising a branch checkout", func(t *testing.T) {
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
		if res.Code != gitops.CodeJujutsuWriteRefused {
			t.Errorf("code = %q, want %q", res.Code, gitops.CodeJujutsuWriteRefused)
		}
		if !strings.Contains(res.Message, core.JujutsuPushCommand) {
			t.Errorf("the refusal does not name the jj command: %s", res.Message)
		}
		if strings.Contains(res.Message, "check out the branch") {
			t.Errorf("the refusal still gives git advice: %s", res.Message)
		}
	})
}
