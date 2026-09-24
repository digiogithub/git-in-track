package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// `gintrack migrate --to N` (GIT-US-0143, R-EVO-4).

const projectYAMLPath = "docs/.pmngr/project.yaml"

// addSecondProject registers a second repository holding project SHOP.
func (h *harness) addSecondProject(schema string) string {
	h.t.Helper()
	repo := filepath.Join(filepath.Dir(h.Repo), "shop")
	dir := filepath.Join(repo, "docs", ".pmngr")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		h.t.Fatalf("mkdir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		h.t.Fatalf("mkdir: %v", err)
	}
	yaml := "schema: " + schema + "\nkey: SHOP\nname: Shop\nworkflow:\n  statuses:\n" +
		"    - {id: todo, name: To Do, category: todo}\n    - {id: done, name: Done, category: done}\n"
	if err := os.WriteFile(filepath.Join(dir, "project.yaml"), []byte(yaml), 0o644); err != nil {
		h.t.Fatalf("write: %v", err)
	}
	h.mustRun("add", repo)
	return repo
}

func TestMigrate(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(h *harness)
		args     []string
		code     int
		stdout   []string // substrings of stdout
		stderr   string   // substring of stderr
		schema   string   // the line project.yaml of DEMO holds afterwards
		changed  int      // --json: projects changed; -1 skips the check
		projects int      // --json: projects reported
	}{
		{name: "raises one project", args: []string{"migrate", "--to", "2"},
			stdout: []string{"DEMO", "migrated schema 1 -> 2", "  - schema: 1", "  + schema: 2", "commit it"}, schema: "schema: 2", changed: -1},
		{name: "dry run writes nothing", args: []string{"migrate", "--to", "2", "--dry-run"},
			stdout: []string{"would migrate schema 1 -> 2", "nothing was changed (--dry-run)"}, schema: "schema: 1", changed: -1},
		{name: "json", args: []string{"migrate", "--to", "2", "--json"}, schema: "schema: 2", changed: 1, projects: 1},
		{name: "json dry run", args: []string{"migrate", "--to", "2", "--json", "--dry-run"}, schema: "schema: 1", changed: 1, projects: 1},
		{name: "idempotent", setup: func(h *harness) { h.mustRun("migrate", "--to", "2") },
			args: []string{"migrate", "--to", "2", "--json"}, schema: "schema: 2", changed: 0, projects: 1},
		{name: "downgrade refused", setup: func(h *harness) { h.mustRun("migrate", "--to", "2") },
			args: []string{"migrate", "--to", "1"}, code: exitValidation, stderr: "refusing to downgrade", schema: "schema: 2", changed: -1},
		{name: "unsupported target", args: []string{"migrate", "--to", "3"}, code: exitUsage,
			stderr: "not a target this build can write", schema: "schema: 1", changed: -1},
		{name: "--to is required", args: []string{"migrate"}, code: exitUsage, stderr: "--to is required", schema: "schema: 1", changed: -1},
		{name: "several projects need a selector", setup: func(h *harness) { h.addSecondProject("1") },
			args: []string{"migrate", "--to", "2"}, code: exitUsage, stderr: "--project or --all is required", schema: "schema: 1", changed: -1},
		{name: "--project selects one", setup: func(h *harness) { h.addSecondProject("1") },
			args: []string{"migrate", "--to", "2", "--project", "SHOP", "--json"}, schema: "schema: 1", changed: 1, projects: 1},
		{name: "--all migrates every project", setup: func(h *harness) { h.addSecondProject("1") },
			args: []string{"migrate", "--to", "2", "--all", "--json"}, schema: "schema: 2", changed: 2, projects: 2},
		{name: "--all writes nothing when one project refuses", setup: func(h *harness) { h.addSecondProject("9") },
			args: []string{"migrate", "--to", "2", "--all"}, code: exitValidation, stderr: "SHOP", schema: "schema: 1", changed: -1},
		{name: "unknown project", args: []string{"migrate", "--to", "2", "--project", "NOPE"}, code: exitNotFound, schema: "schema: 1", changed: -1},
		{name: "--project and --all conflict", args: []string{"migrate", "--to", "2", "--all", "--project", "DEMO"}, code: exitUsage, schema: "schema: 1", changed: -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			h.register()
			if tt.setup != nil {
				tt.setup(h)
			}
			stdout, stderr, code := h.run(tt.args...)
			if code != tt.code {
				t.Fatalf("exit %d, want %d\nstdout:\n%s\nstderr:\n%s", code, tt.code, stdout, stderr)
			}
			for _, want := range tt.stdout {
				if !strings.Contains(stdout, want) {
					t.Errorf("stdout lacks %q:\n%s", want, stdout)
				}
			}
			if tt.stderr != "" && !strings.Contains(stderr, tt.stderr) {
				t.Errorf("stderr lacks %q:\n%s", tt.stderr, stderr)
			}
			if got := h.readFile(projectYAMLPath); !strings.Contains(got, "\n"+tt.schema+"\n") {
				t.Errorf("project.yaml lacks %q:\n%s", tt.schema, got)
			}
			if tt.changed >= 0 {
				payload := decode[migratePayload](t, stdout)
				if payload.Changed != tt.changed || len(payload.Projects) != tt.projects {
					t.Errorf("changed %d of %d projects, want %d of %d", payload.Changed, len(payload.Projects), tt.changed, tt.projects)
				}
			}
		})
	}
}

// TestMigrateChangesOneLine checks that the upgrade is one reviewable line:
// comments and every other byte of project.yaml survive.
func TestMigrateChangesOneLine(t *testing.T) {
	h := newHarness(t)
	h.register()
	before := h.readFile(projectYAMLPath)
	h.mustRun("migrate", "--to", "2")
	after := h.readFile(projectYAMLPath)
	if want := strings.Replace(before, "\nschema: 1\n", "\nschema: 2\n", 1); after != want {
		t.Errorf("project.yaml changed beyond the schema line:\n%s", after)
	}
}
