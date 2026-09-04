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
	"github.com/digiogithub/git-in-track/internal/gitops"
)

// gitSettingsBody is the documented shape of /api/v1/git/settings.
type gitSettingsBody struct {
	CommitOnSave     bool   `json:"commitOnSave"`
	CommitDebounceMs int    `json:"commitDebounceMs"`
	MessageTemplate  string `json:"messageTemplate"`
	Backend          string `json:"backend"`
	ResolvedBackend  string `json:"resolvedBackend"`
	Pending          int    `json:"pending"`
	Persisted        bool   `json:"persisted"`
}

// gitStatusBody is the documented shape of /api/v1/git/status.
type gitStatusBody struct {
	Repos []struct {
		Repo          string `json:"repo"`
		Git           bool   `json:"git"`
		Reason        string `json:"reason"`
		Backend       string `json:"backend"`
		Identity      string `json:"identity"`
		IdentityError string `json:"identityError"`
		Capabilities  struct {
			Hooks   bool `json:"hooks"`
			Signing bool `json:"signing"`
		} `json:"capabilities"`
	} `json:"repos"`
	Settings gitSettingsBody `json:"settings"`
}

// gitCommitBody is the documented shape of POST /api/v1/git/commit.
type gitCommitBody struct {
	Commits []struct {
		Repo    string `json:"repo"`
		SHA     string `json:"sha"`
		Subject string `json:"subject"`
		Empty   bool   `json:"empty"`
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"commits"`
}

// initGitRepo turns a directory into a working tree with an identity and one
// commit, so the fixture behaves like a real clone.
func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	bin, err := exec.LookPath("git")
	if err != nil {
		t.Skipf("these tests need a git binary: %v", err)
	}
	run := func(args ...string) {
		t.Helper()
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
	run("init", "--initial-branch=main")
	run("config", "user.name", "Test User")
	run("config", "user.email", "test@example.com")
	run("config", "commit.gpgsign", "false")
	run("add", "-A")
	run("commit", "-m", "chore: seed the fixture")
}

// gitLog returns the commit subjects of a repository, newest first.
func gitLog(t *testing.T, dir string) []string {
	t.Helper()
	out, err := exec.CommandContext(t.Context(), "git", "-C", dir, "log", "--format=%s").Output()
	if err != nil {
		t.Fatalf("git log: %v", err)
	}
	text := strings.TrimSpace(string(out))
	if text == "" {
		return nil
	}
	return strings.Split(text, "\n")
}

// gitBody returns the body of the newest commit.
func gitBody(t *testing.T, dir string) string {
	t.Helper()
	out, err := exec.CommandContext(t.Context(), "git", "-C", dir, "log", "-1", "--format=%b").Output()
	if err != nil {
		t.Fatalf("git log: %v", err)
	}
	return string(out)
}

// newGitServer mounts the fixture as a real git repository and serves it with
// the given git settings.
func newGitServer(t *testing.T, git config.Git) (*Server, string) {
	t.Helper()
	root := copyTree(t, fixtureRoot)
	initGitRepo(t, root)
	s, err := New(Options{
		Token:     "test-token",
		Version:   "0.0.1-test",
		Workspace: "test",
		Repos:     []Repo{{ID: testRepoID, Path: root, Role: "project", DocsFolder: "docs"}},
		Now:       func() time.Time { return time.Date(2026, 9, 4, 10, 0, 0, 0, time.UTC) },
		Git:       git,
	})
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	t.Cleanup(func() { s.git.close(t.Context()) })
	return s, root
}

func TestGitSettingsSurface(t *testing.T) {
	t.Run("the defaults are reported with commit-on-save off", func(t *testing.T) {
		s, _ := newGitServer(t, config.Default().Git)
		var body gitSettingsBody
		decode(t, send(t, s, request{method: http.MethodGet, target: "/api/v1/git/settings"}),
			http.StatusOK, &body)

		if body.CommitOnSave {
			t.Error("commit-on-save must be off by default")
		}
		if body.CommitDebounceMs != 2000 {
			t.Errorf("commitDebounceMs = %d, want 2000", body.CommitDebounceMs)
		}
		if body.MessageTemplate != config.DefaultCommitMessageTemplate {
			t.Errorf("messageTemplate = %q", body.MessageTemplate)
		}
		if body.Backend != string(config.BackendAuto) {
			t.Errorf("backend = %q, want auto", body.Backend)
		}
		if body.ResolvedBackend != string(gitops.KindSystem) && body.ResolvedBackend != string(gitops.KindGoGit) {
			t.Errorf("resolvedBackend = %q, want a concrete backend", body.ResolvedBackend)
		}
	})

	tests := []struct {
		name    string
		patch   map[string]any
		status  int
		wantOn  bool
		wantTpl string
	}{
		{
			name:    "commit-on-save is turned on",
			patch:   map[string]any{"commitOnSave": true},
			status:  http.StatusOK,
			wantOn:  true,
			wantTpl: config.DefaultCommitMessageTemplate,
		},
		{
			name:    "the template is replaced",
			patch:   map[string]any{"messageTemplate": "{{action}} {{id}}: {{title}}"},
			status:  http.StatusOK,
			wantTpl: "{{action}} {{id}}: {{title}}",
		},
		{
			name:   "a broken template is refused",
			patch:  map[string]any{"messageTemplate": "{{nosuchplaceholder}}"},
			status: http.StatusBadRequest,
		},
		{
			name:   "a negative debounce is refused",
			patch:  map[string]any{"commitDebounceMs": -1},
			status: http.StatusBadRequest,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := newGitServer(t, config.Default().Git)
			var body gitSettingsBody
			rec := send(t, s, request{method: http.MethodPatch, target: "/api/v1/git/settings", body: tc.patch})
			if tc.status != http.StatusOK {
				decode(t, rec, tc.status, nil)
				// The rejected setting must not have been applied.
				decode(t, send(t, s, request{method: http.MethodGet, target: "/api/v1/git/settings"}),
					http.StatusOK, &body)
				if body.MessageTemplate != config.DefaultCommitMessageTemplate {
					t.Errorf("a refused patch changed the template to %q", body.MessageTemplate)
				}
				return
			}
			decode(t, rec, http.StatusOK, &body)
			if body.CommitOnSave != tc.wantOn {
				t.Errorf("commitOnSave = %v, want %v", body.CommitOnSave, tc.wantOn)
			}
			if body.MessageTemplate != tc.wantTpl {
				t.Errorf("messageTemplate = %q, want %q", body.MessageTemplate, tc.wantTpl)
			}
		})
	}
}

func TestGitSettingsArePersisted(t *testing.T) {
	root := copyTree(t, fixtureRoot)
	initGitRepo(t, root)
	configPath := filepath.Join(t.TempDir(), "config.yaml")

	s, err := New(Options{
		Token:      "test-token",
		Version:    "0.0.1-test",
		Workspace:  "test",
		Repos:      []Repo{{ID: testRepoID, Path: root, Role: "project", DocsFolder: "docs"}},
		Git:        config.Default().Git,
		ConfigPath: configPath,
	})
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	t.Cleanup(func() { s.git.close(t.Context()) })

	var body gitSettingsBody
	decode(t, send(t, s, request{
		method: http.MethodPatch,
		target: "/api/v1/git/settings",
		body:   map[string]any{"commitOnSave": true, "commitDebounceMs": 250},
	}), http.StatusOK, &body)

	if !body.Persisted {
		t.Fatal("the change should have been written to the configuration file")
	}
	saved, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !saved.Git.CommitOnSave || saved.Git.CommitDebounce != 250*time.Millisecond {
		t.Errorf("saved git section = %+v", saved.Git)
	}
}

func TestGitStatus(t *testing.T) {
	tests := []struct {
		name   string
		asRepo bool
		wantOK bool
	}{
		{name: "a working tree reports its backend and identity", asRepo: true, wantOK: true},
		{name: "a plain folder reports why it has no git", asRepo: false, wantOK: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := copyTree(t, fixtureRoot)
			if tc.asRepo {
				initGitRepo(t, root)
			}
			s, err := New(Options{
				Token:     "test-token",
				Version:   "0.0.1-test",
				Workspace: "test",
				Repos:     []Repo{{ID: testRepoID, Path: root, Role: "project", DocsFolder: "docs"}},
				Git:       config.Default().Git,
			})
			if err != nil {
				t.Fatalf("New(): %v", err)
			}
			t.Cleanup(func() { s.git.close(t.Context()) })

			var body gitStatusBody
			decode(t, send(t, s, request{method: http.MethodGet, target: "/api/v1/git/status"}),
				http.StatusOK, &body)
			if len(body.Repos) != 1 {
				t.Fatalf("repos = %+v", body.Repos)
			}
			got := body.Repos[0]
			if got.Git != tc.wantOK {
				t.Fatalf("git = %v, want %v (%s)", got.Git, tc.wantOK, got.Reason)
			}
			if !tc.wantOK {
				if got.Reason == "" {
					t.Error("a repository without git must say why")
				}
				return
			}
			if got.Identity != "Test User <test@example.com>" {
				t.Errorf("identity = %q, want the repository configuration", got.Identity)
			}
			if got.Backend == string(gitops.KindSystem) && !got.Capabilities.Hooks {
				t.Error("the system backend must advertise hooks")
			}
		})
	}

	t.Run("an unknown repository is a problem", func(t *testing.T) {
		s, _ := newGitServer(t, config.Default().Git)
		decode(t, send(t, s, request{method: http.MethodGet, target: "/api/v1/git/status?repo=nope"}),
			http.StatusNotFound, nil)
	})
}

func TestCommitOnSaveThroughTheAPI(t *testing.T) {
	// A zero window would still be asynchronous; the tests flush explicitly, so
	// an hour-long window proves the batching rather than a lucky timing.
	settings := config.Default().Git
	settings.CommitOnSave = true
	settings.CommitDebounce = time.Hour

	t.Run("commit-on-save is off unless it is turned on", func(t *testing.T) {
		off := config.Default().Git
		s, root := newGitServer(t, off)
		before := len(gitLog(t, root))
		createFixtureItem(t, s, "Off by default")
		s.git.flush(t.Context())
		if after := len(gitLog(t, root)); after != before {
			t.Errorf("commit count went from %d to %d with commit-on-save off", before, after)
		}
	})

	t.Run("a create is one commit rendered from the template", func(t *testing.T) {
		s, root := newGitServer(t, settings)
		before := len(gitLog(t, root))
		id := createFixtureItem(t, s, "Wire OIDC discovery")
		s.git.flush(t.Context())

		log := gitLog(t, root)
		if len(log)-before != 1 {
			t.Fatalf("%d commits, want exactly one: %v", len(log)-before, log)
		}
		if want := `pmngr: update ` + id + ` "Wire OIDC discovery"`; log[0] != want {
			t.Errorf("subject = %q, want %q", log[0], want)
		}
		body := gitBody(t, root)
		for _, want := range []string{"Item: " + id, "Type: task", "Tool: gintrack 0.0.1-test (companion)"} {
			if !strings.Contains(body, want) {
				t.Errorf("the trailers are missing %q:\n%s", want, body)
			}
		}
	})

	t.Run("rapid edits of one item are one commit", func(t *testing.T) {
		s, root := newGitServer(t, settings)
		id := createFixtureItem(t, s, "Batched")
		s.git.flush(t.Context())
		before := len(gitLog(t, root))

		rev := itemRev(t, s, id)
		for i := range 8 {
			var updated struct {
				Rev string `json:"rev"`
			}
			decode(t, send(t, s, request{
				method: http.MethodPatch,
				target: "/api/v1/items/" + id,
				body:   map[string]any{"body": "## Description\nrevision " + string(rune('a'+i)) + "\n"},
				header: map[string]string{"If-Match": rev},
			}), http.StatusOK, &updated)
			rev = updated.Rev
		}
		if pending := s.git.pending(); pending != 1 {
			t.Fatalf("pending batches = %d, want 1: eight edits must coalesce", pending)
		}
		s.git.flush(t.Context())
		if made := len(gitLog(t, root)) - before; made != 1 {
			t.Errorf("eight edits produced %d commits, want 1", made)
		}
	})

	t.Run("only the files the edit touched are staged", func(t *testing.T) {
		s, root := newGitServer(t, settings)
		// A file nobody asked us to commit, left dirty in the working tree.
		stray := filepath.Join(root, "untracked-note.md")
		if err := os.WriteFile(stray, []byte("not ours\n"), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
		createFixtureItem(t, s, "Only mine")
		s.git.flush(t.Context())

		out, err := exec.CommandContext(t.Context(), "git", "-C", root, "status", "--porcelain").Output()
		if err != nil {
			t.Fatalf("git status: %v", err)
		}
		if !strings.Contains(string(out), "untracked-note.md") {
			t.Errorf("the stray file must still be uncommitted:\n%s", out)
		}
	})

	t.Run("an explicit commit flushes what is pending", func(t *testing.T) {
		s, root := newGitServer(t, settings)
		before := len(gitLog(t, root))
		createFixtureItem(t, s, "Flushed explicitly")

		var body gitCommitBody
		decode(t, send(t, s, request{method: http.MethodPost, target: "/api/v1/git/commit"}),
			http.StatusOK, &body)
		if len(body.Commits) != 1 || body.Commits[0].SHA == "" {
			t.Fatalf("commits = %+v, want one commit", body.Commits)
		}
		if made := len(gitLog(t, root)) - before; made != 1 {
			t.Errorf("%d commits, want 1", made)
		}
	})
}

func TestCommitOnSaveNeverLosesContent(t *testing.T) {
	settings := config.Default().Git
	settings.CommitOnSave = true
	settings.CommitDebounce = time.Hour

	s, root := newGitServer(t, settings)
	// A hook that always refuses turns every commit into a failure; the write
	// itself must still have landed on disk (AC 7).
	hook := filepath.Join(root, ".git", "hooks", "pre-commit")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\necho refused >&2\nexit 1\n"), 0o755); err != nil { //nolint:gosec // a hook has to be executable
		t.Fatalf("write the hook: %v", err)
	}
	if s.git.view().ResolvedBackend != string(gitops.KindSystem) {
		t.Skip("hooks only run with the system backend")
	}

	id := createFixtureItem(t, s, "Survives a refused commit")
	outcomes := s.git.flush(t.Context())
	if len(outcomes) != 1 || outcomes[0].Err == nil {
		t.Fatalf("outcomes = %+v, want one failure", outcomes)
	}
	if outcomes[0].Code != gitops.CodeHookFailed {
		t.Errorf("code = %q, want %q", outcomes[0].Code, gitops.CodeHookFailed)
	}

	var item struct {
		Path string `json:"path"`
	}
	decode(t, send(t, s, request{method: http.MethodGet, target: "/api/v1/items/" + id}), http.StatusOK, &item)
	if _, err := os.Stat(filepath.Join(root, item.Path)); err != nil {
		t.Fatalf("the item file must survive a refused commit: %v", err)
	}
}

// createFixtureItem creates a task through the API and returns its id.
func createFixtureItem(t *testing.T, s *Server, title string) string {
	t.Helper()
	var created struct {
		ID string `json:"id"`
	}
	decode(t, send(t, s, request{
		method: http.MethodPost,
		target: "/api/v1/items",
		body:   map[string]any{"type": "task", "title": title},
	}), http.StatusCreated, &created)
	if created.ID == "" {
		t.Fatal("the created item has no id")
	}
	return created.ID
}

// itemRev reads the current revision of an item.
func itemRev(t *testing.T, s *Server, id string) string {
	t.Helper()
	var item struct {
		Rev string `json:"rev"`
	}
	decode(t, send(t, s, request{method: http.MethodGet, target: "/api/v1/items/" + id}), http.StatusOK, &item)
	return item.Rev
}

// TestCommitOnSaveAcrossRepositories checks the cross-repository rule of
// docs/06 section 9.4: a card move writes the item in its project clone and the
// board in the team repository, so it produces two commits — one per repository
// — and never a commit that spans both.
func TestCommitOnSaveAcrossRepositories(t *testing.T) {
	teamRoot := copyTree(t, teamFixtureRoot)
	projectRoot := copyTree(t, fixtureRoot)
	initGitRepo(t, teamRoot)
	initGitRepo(t, projectRoot)

	settings := config.Default().Git
	settings.CommitOnSave = true
	settings.CommitDebounce = time.Hour
	settings.MessageTemplate = "{{action}} {{id}} to {{status}}"

	s, err := New(Options{
		Token:     "test-token",
		Version:   "0.0.1-test",
		Workspace: "test",
		Repos: []Repo{
			{ID: teamRepoID, Path: teamRoot, Role: "team", DocsFolder: "knowledge"},
			{ID: testRepoID, Path: projectRoot, Role: "project", DocsFolder: "docs"},
		},
		Now: func() time.Time { return time.Date(2026, 9, 4, 10, 0, 0, 0, time.UTC) },
		Git: settings,
	})
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	t.Cleanup(func() { s.git.close(t.Context()) })

	teamBefore, projectBefore := len(gitLog(t, teamRoot)), len(gitLog(t, projectRoot))

	var view boardViewBody
	decode(t, send(t, s, request{method: http.MethodGet, target: "/api/v1/boards/delivery"}),
		http.StatusOK, &view)
	var moved boardMoveBody
	decode(t, send(t, s, request{
		method: http.MethodPost, target: "/api/v1/boards/delivery/cards/move",
		header: map[string]string{"If-Match": view.Rev},
		body: map[string]any{
			"ref": "DEMO/DEMO-US-0002", "toColumn": "in_progress", "position": 0, "force": true,
		},
	}), http.StatusOK, &moved)

	if pending := s.git.pending(); pending != 2 {
		t.Fatalf("pending batches = %d, want one per repository", pending)
	}
	outcomes := s.git.flush(t.Context())
	for _, out := range outcomes {
		if out.Err != nil {
			t.Fatalf("%s: %v", out.Repo, out.Err)
		}
	}

	tests := []struct {
		name   string
		root   string
		before int
	}{
		{name: "the project repository committed the item", root: projectRoot, before: projectBefore},
		{name: "the team repository committed the board", root: teamRoot, before: teamBefore},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			log := gitLog(t, tc.root)
			if made := len(log) - tc.before; made != 1 {
				t.Fatalf("%d commits, want exactly one: %v", made, log)
			}
			if want := "move DEMO-US-0002 to in_progress"; log[0] != want {
				t.Errorf("subject = %q, want %q", log[0], want)
			}
		})
	}
}
