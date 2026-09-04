package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// `gintrack sync` against a real bare remote and two clones (GIT-US-0021).

// syncHarness is a harness whose repository is a clone of a bare remote, plus a
// second clone standing in for a teammate.
type syncHarness struct {
	*harness
	peer string
}

// git runs git in dir and fails the test on error.
func gitIn(t *testing.T, dir string, args ...string) {
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
	if out, runErr := cmd.CombinedOutput(); runErr != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), runErr, out)
	}
}

// newSyncHarness builds the remote, the clones and the registration.
func newSyncHarness(t *testing.T) *syncHarness {
	t.Helper()
	root := t.TempDir()
	seed := filepath.Join(root, "seed")
	copyTree(t, filepath.Join("..", "..", "testdata", "fixtures", fixtureName), seed)
	gitIn(t, seed, "init", "--initial-branch=main")
	identify(t, seed)
	gitIn(t, seed, "add", "-A")
	gitIn(t, seed, "commit", "-m", "chore: seed")

	remote := filepath.Join(root, "remote.git")
	gitIn(t, root, "clone", "--bare", seed, remote)
	local := filepath.Join(root, "acme-api")
	peer := filepath.Join(root, "peer")
	gitIn(t, root, "clone", remote, local)
	gitIn(t, root, "clone", remote, peer)
	identify(t, local)
	identify(t, peer)

	cfg := filepath.Join(root, "state", "config.yaml")
	t.Setenv("GINTRACK_CONFIG", cfg)
	h := &syncHarness{harness: &harness{t: t, Repo: local, Config: cfg}, peer: peer}
	h.register()
	return h
}

// identify gives a clone the identity its commits need.
func identify(t *testing.T, dir string) {
	t.Helper()
	gitIn(t, dir, "config", "user.name", "Test User")
	gitIn(t, dir, "config", "user.email", "test@example.com")
	gitIn(t, dir, "config", "commit.gpgsign", "false")
}

// peerPush commits a file in the teammate's clone and publishes it.
func (h *syncHarness) peerPush(rel, text, subject string) {
	h.t.Helper()
	target := filepath.Join(h.peer, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		h.t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(target, []byte(text), 0o644); err != nil {
		h.t.Fatalf("write: %v", err)
	}
	gitIn(h.t, h.peer, "add", rel)
	gitIn(h.t, h.peer, "commit", "-m", subject)
	gitIn(h.t, h.peer, "push", "origin", "main")
}

// syncJSON runs `gintrack sync --json` with extra flags and decodes the report.
func (h *syncHarness) syncJSON(args ...string) (syncPayload, int, string) {
	h.t.Helper()
	stdout, stderr, code := h.run(append([]string{"sync", "--json"}, args...)...)
	var payload syncPayload
	if strings.TrimSpace(stdout) != "" {
		if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
			h.t.Fatalf("decode: %v\n%s", err, stdout)
		}
	}
	return payload, code, stderr
}

func TestSyncCommand(t *testing.T) {
	t.Run("a dry run previews the incoming work and changes nothing", func(t *testing.T) {
		h := newSyncHarness(t)
		h.peerPush("docs/incoming.md", "incoming\n", "docs: teammate work")

		payload, code, stderr := h.syncJSON("--dry-run")
		if code != exitOK {
			t.Fatalf("exit %d: %s", code, stderr)
		}
		if !payload.DryRun || len(payload.Repos) != 1 {
			t.Fatalf("payload = %+v", payload)
		}
		res := payload.Repos[0].Result
		if res == nil || len(res.Incoming) != 1 || res.Incoming[0].Subject != "docs: teammate work" {
			t.Fatalf("result = %+v", res)
		}
		if payload.Pulled != 0 || payload.Pushed != 0 {
			t.Fatalf("a dry run moved commits: %+v", payload)
		}
		if _, err := os.Stat(filepath.Join(h.Repo, "docs", "incoming.md")); err == nil {
			t.Fatal("the dry run wrote into the working tree")
		}
	})

	t.Run("a run integrates and reports what moved", func(t *testing.T) {
		h := newSyncHarness(t)
		h.peerPush("docs/incoming.md", "incoming\n", "docs: teammate work")

		payload, code, stderr := h.syncJSON()
		if code != exitOK {
			t.Fatalf("exit %d: %s", code, stderr)
		}
		if payload.Pulled != 1 {
			t.Fatalf("pulled = %d (%+v)", payload.Pulled, payload.Repos[0].Result)
		}
		if _, err := os.Stat(filepath.Join(h.Repo, "docs", "incoming.md")); err != nil {
			t.Fatalf("the incoming file did not land: %v", err)
		}
	})

	t.Run("--commit-all commits the working tree and publishes it", func(t *testing.T) {
		h := newSyncHarness(t)
		if err := os.WriteFile(filepath.Join(h.Repo, "docs", "index.md"), []byte("# edited\n"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}

		payload, code, stderr := h.syncJSON("--commit-all", "--message", "docs: edit the index")
		if code != exitOK {
			t.Fatalf("exit %d: %s", code, stderr)
		}
		if payload.Pushed != 1 {
			t.Fatalf("pushed = %d (%+v)", payload.Pushed, payload.Repos[0].Result)
		}
		gitIn(t, h.peer, "pull", "--ff-only", "origin", "main")
		data, err := os.ReadFile(filepath.Join(h.peer, "docs", "index.md"))
		if err != nil || !strings.Contains(string(data), "edited") {
			t.Fatalf("the teammate did not receive the edit: %q (%v)", data, err)
		}
	})

	t.Run("an uncommitted edit refuses the run and exits 6", func(t *testing.T) {
		h := newSyncHarness(t)
		if err := os.WriteFile(filepath.Join(h.Repo, "docs", "index.md"), []byte("# edited\n"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
		payload, code, _ := h.syncJSON()
		if code != exitGit {
			t.Fatalf("exit %d, want %d", code, exitGit)
		}
		if payload.Repos[0].Result.Code != "git_dirty_tree" {
			t.Fatalf("code = %q", payload.Repos[0].Result.Code)
		}
	})

	t.Run("a conflict exits 5, names the file and stays recoverable", func(t *testing.T) {
		h := newSyncHarness(t)
		h.peerPush("docs/same.md", "theirs\n", "docs: theirs")
		if err := os.WriteFile(filepath.Join(h.Repo, "docs", "same.md"), []byte("mine\n"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
		gitIn(t, h.Repo, "add", "docs/same.md")
		gitIn(t, h.Repo, "commit", "-m", "docs: mine")

		payload, code, _ := h.syncJSON()
		if code != exitConflict {
			t.Fatalf("exit %d, want %d (%+v)", code, exitConflict, payload.Repos[0].Result)
		}
		res := payload.Repos[0].Result
		if len(res.Conflicts) != 1 || res.Conflicts[0].Path != "docs/same.md" {
			t.Fatalf("conflicts = %+v", res.Conflicts)
		}

		// --abort restores the tree exactly as it was.
		if _, _, code := h.run("sync", "--abort"); code != exitOK {
			t.Fatalf("abort: exit %d", code)
		}
		data, err := os.ReadFile(filepath.Join(h.Repo, "docs", "same.md"))
		if err != nil || strings.TrimSpace(string(data)) != "mine" {
			t.Fatalf("the abort did not restore the local version: %q (%v)", data, err)
		}
	})

	t.Run("an unknown strategy is a usage error", func(t *testing.T) {
		h := newSyncHarness(t)
		if _, _, code := h.run("sync", "--strategy", "octopus"); code != exitUsage {
			t.Fatalf("exit %d, want %d", code, exitUsage)
		}
	})

	t.Run("an unknown repository is not found", func(t *testing.T) {
		h := newSyncHarness(t)
		if _, _, code := h.run("sync", "--repo", "nope"); code != exitNotFound {
			t.Fatalf("exit %d, want %d", code, exitNotFound)
		}
	})
}
