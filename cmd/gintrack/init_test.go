package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// emptyRepo returns a harness pointed at a git working tree with no backlog.
func emptyRepo(t *testing.T) (*harness, string) {
	t.Helper()
	h := newHarness(t)
	repo := filepath.Join(t.TempDir(), "greenfield")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatalf("create the working tree: %v", err)
	}
	return h, repo
}

func TestInitCreatesAProject(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "in the default docs folder", args: []string{"--key", "ACME"}, want: "docs/.pmngr/project.yaml"},
		{name: "at the repository root", args: []string{"--key", "ACME", "--docs", "."}, want: ".pmngr/project.yaml"},
		{name: "in a monorepo folder", args: []string{"--key", "API", "--docs", "apps/api/docs"}, want: "apps/api/docs/.pmngr/project.yaml"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h, repo := emptyRepo(t)
			h.mustRun(append([]string{"init", repo}, tc.args...)...)
			if _, err := os.Stat(filepath.Join(repo, filepath.FromSlash(tc.want))); err != nil {
				t.Fatalf("no project at %s: %v", tc.want, err)
			}
		})
	}
}

func TestInitRefusals(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want int
	}{
		{name: "no key at all", args: []string{"init"}, want: exitUsage},
		{name: "a key the grammar refuses", args: []string{"init", "--key", "acme"}, want: exitValidation},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h, repo := emptyRepo(t)
			args := append([]string{tc.args[0], repo}, tc.args[1:]...)
			if _, stderr, code := h.run(args...); code != tc.want {
				t.Fatalf("exit %d, want %d\n%s", code, tc.want, stderr)
			}
		})
	}

	t.Run("an existing project is never overwritten", func(t *testing.T) {
		h, repo := emptyRepo(t)
		h.mustRun("init", repo, "--key", "ACME")
		if _, stderr, code := h.run("init", repo, "--key", "OTHER"); code != exitConflict {
			t.Fatalf("exit %d, want %d\n%s", code, exitConflict, stderr)
		}
	})
}

func TestInitRegistersTheRepository(t *testing.T) {
	h, repo := emptyRepo(t)
	out := h.mustRun("init", repo, "--key", "ACME", "--name", "ACME Platform", "--register", "--json")

	var payload initPayload
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("decode --json: %v\n%s", err, out)
	}
	if payload.Key != "ACME" || payload.Name != "ACME Platform" || payload.Repo == nil {
		t.Fatalf("payload = %+v", payload)
	}
	if got := h.mustRun("ls"); !strings.Contains(got, "ACME") {
		t.Errorf("ls does not list the new project:\n%s", got)
	}
}

func TestAddCreatesAProjectWithKey(t *testing.T) {
	h, repo := emptyRepo(t)
	out := h.mustRun("add", repo, "--key", "ACME", "--name", "ACME Platform", "--json")

	var payload addPayload
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("decode --json: %v\n%s", err, out)
	}
	if payload.Created == nil || payload.Created.Key != "ACME" {
		t.Fatalf("created = %+v", payload.Created)
	}
	if len(payload.Projects) != 1 || payload.Projects[0] != "ACME" {
		t.Errorf("projects = %v, want [ACME]", payload.Projects)
	}
}

func TestAddDoesNotImportFixturesItOnlyDetected(t *testing.T) {
	h, repo := emptyRepo(t)
	// A backlog two levels down is a fixture, not a project of this repository.
	deep := filepath.Join(repo, "testdata", "sample", "docs", ".pmngr")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatalf("seed the fixture: %v", err)
	}
	body := "schema: 1\nkey: DEMO\nworkflow:\n  statuses:\n    - {id: todo, category: todo}\n    - {id: done, category: done}\n"
	if err := os.WriteFile(filepath.Join(deep, "project.yaml"), []byte(body), 0o600); err != nil {
		t.Fatalf("seed the fixture: %v", err)
	}

	h.mustRun("add", repo, "--key", "ACME")
	listing := h.mustRun("ls")
	if !strings.Contains(listing, "ACME") {
		t.Errorf("ls lost the real project:\n%s", listing)
	}
	if strings.Contains(listing, "DEMO") {
		t.Errorf("ls reports a fixture as a project:\n%s", listing)
	}
}

func TestAddDeclaresRepeatedDocsFolders(t *testing.T) {
	h, repo := emptyRepo(t)
	h.mustRun("init", repo, "--key", "API", "--docs", "apps/api/docs")
	h.mustRun("init", repo, "--key", "WEB", "--docs", "apps/web/docs")

	out := h.mustRun("add", repo, "--docs", "apps/api/docs", "--docs", "apps/web/docs", "--json")
	var payload addPayload
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("decode --json: %v\n%s", err, out)
	}
	if len(payload.Projects) != 2 {
		t.Fatalf("projects = %v, want both declared monorepo projects", payload.Projects)
	}
}
