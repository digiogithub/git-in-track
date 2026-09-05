package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitTeamCreatesATeamRepository(t *testing.T) {
	t.Run("writes team.yaml and the team layout", func(t *testing.T) {
		h, repo := emptyRepo(t)
		out := h.mustRun("init", repo, "--team", "--key", "ACME-TEAM", "--name", "ACME Delivery Team", "--json")

		var payload teamPayload
		if err := json.Unmarshal([]byte(out), &payload); err != nil {
			t.Fatalf("decode --json: %v\n%s", err, out)
		}
		if payload.Key != "ACME-TEAM" || payload.Name != "ACME Delivery Team" {
			t.Fatalf("payload = %+v", payload)
		}
		for _, rel := range []string{"team.yaml", "knowledge/index.md"} {
			if _, err := os.Stat(filepath.Join(repo, filepath.FromSlash(rel))); err != nil {
				t.Errorf("%s was not written: %v", rel, err)
			}
		}
		for _, rel := range []string{".pmngr/boards", ".pmngr/sprints", ".pmngr/retros", ".pmngr/index"} {
			info, err := os.Stat(filepath.Join(repo, filepath.FromSlash(rel)))
			if err != nil || !info.IsDir() {
				t.Errorf("%s is not a directory: %v", rel, err)
			}
		}
	})

	t.Run("a custom knowledge folder is honored", func(t *testing.T) {
		h, repo := emptyRepo(t)
		h.mustRun("init", repo, "--team", "--key", "ACME", "--knowledge", "wiki")
		if _, err := os.Stat(filepath.Join(repo, "wiki", "index.md")); err != nil {
			t.Fatalf("wiki/index.md: %v", err)
		}
	})

	t.Run("--register records the team role", func(t *testing.T) {
		h, repo := emptyRepo(t)
		out := h.mustRun("init", repo, "--team", "--key", "ACME-TEAM", "--register", "--json")
		var payload teamPayload
		if err := json.Unmarshal([]byte(out), &payload); err != nil {
			t.Fatalf("decode --json: %v\n%s", err, out)
		}
		if payload.Repo == nil || payload.Repo.Role != "team" {
			t.Fatalf("repo = %+v", payload.Repo)
		}
	})

	t.Run("refusals", func(t *testing.T) {
		tests := []struct {
			name string
			args []string
			want int
		}{
			{name: "no key at all", args: []string{"--team"}, want: exitUsage},
			{name: "a key the grammar refuses", args: []string{"--team", "--key", "acme team"}, want: exitValidation},
		}
		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				h, repo := emptyRepo(t)
				if _, stderr, code := h.run(append([]string{"init", repo}, tc.args...)...); code != tc.want {
					t.Fatalf("exit %d, want %d\n%s", code, tc.want, stderr)
				}
			})
		}
	})

	t.Run("an existing team repository is never overwritten", func(t *testing.T) {
		h, repo := emptyRepo(t)
		h.mustRun("init", repo, "--team", "--key", "ACME-TEAM")
		if _, stderr, code := h.run("init", repo, "--team", "--key", "OTHER"); code != exitConflict {
			t.Fatalf("exit %d, want %d\n%s", code, exitConflict, stderr)
		}
	})
}

func TestAddTeamNeedsATeamFile(t *testing.T) {
	t.Run("a folder with no team.yaml is refused with the command that fixes it", func(t *testing.T) {
		h, repo := emptyRepo(t)
		_, stderr, code := h.run("add", repo, "--team")
		if code != exitValidation {
			t.Fatalf("exit %d, want %d\n%s", code, exitValidation, stderr)
		}
		if !strings.Contains(stderr, "--team --key") {
			t.Errorf("the refusal does not carry the command that fixes it:\n%s", stderr)
		}
		// Nothing was registered: the configuration is untouched.
		if listing := h.mustRun("ls"); strings.Contains(listing, repo) {
			t.Errorf("the refused folder was registered anyway:\n%s", listing)
		}
	})

	t.Run("--key creates the team repository while registering", func(t *testing.T) {
		h, repo := emptyRepo(t)
		out := h.mustRun("add", repo, "--team", "--key", "ACME-TEAM", "--name", "ACME Delivery Team", "--json")
		var payload addPayload
		if err := json.Unmarshal([]byte(out), &payload); err != nil {
			t.Fatalf("decode --json: %v\n%s", err, out)
		}
		if payload.CreatedTeam == nil || payload.CreatedTeam.Key != "ACME-TEAM" {
			t.Fatalf("createdTeam = %+v", payload.CreatedTeam)
		}
		if payload.Repo.Role != "team" {
			t.Errorf("role = %q, want team", payload.Repo.Role)
		}
		if _, err := os.Stat(filepath.Join(repo, "team.yaml")); err != nil {
			t.Errorf("team.yaml was not written: %v", err)
		}
	})

	t.Run("doctor is clean on a freshly created team repository", func(t *testing.T) {
		h, repo := emptyRepo(t)
		h.mustRun("init", repo, "--team", "--key", "ACME-TEAM", "--register")
		// A team repository holds no backlog of its own, so the missing-project
		// error would be exactly wrong here.
		out := h.mustRun("doctor")
		if strings.Contains(out, "no .pmngr/project.yaml") {
			t.Errorf("doctor asks a team repository for a backlog:\n%s", out)
		}
		if !strings.Contains(out, "team.yaml found") {
			t.Errorf("doctor does not check the team marker:\n%s", out)
		}
	})

	t.Run("doctor reports a team registration with no team.yaml", func(t *testing.T) {
		h, repo := emptyRepo(t)
		h.mustRun("add", repo, "--team", "--key", "ACME-TEAM")
		if err := os.Remove(filepath.Join(repo, "team.yaml")); err != nil {
			t.Fatalf("remove team.yaml: %v", err)
		}
		_, stderr, code := h.run("doctor")
		if code == 0 {
			t.Fatalf("doctor is happy with an inert team registration\n%s", stderr)
		}
	})

	t.Run("an existing team repository registers unchanged", func(t *testing.T) {
		h, repo := emptyRepo(t)
		h.mustRun("init", repo, "--team", "--key", "ACME-TEAM")
		before, err := os.ReadFile(filepath.Join(repo, "team.yaml"))
		if err != nil {
			t.Fatalf("read team.yaml: %v", err)
		}
		h.mustRun("add", repo, "--team")
		after, err := os.ReadFile(filepath.Join(repo, "team.yaml"))
		if err != nil || string(after) != string(before) {
			t.Errorf("registering rewrote team.yaml")
		}
	})
}
