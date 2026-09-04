package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// teamHarness is a harness with the team fixture registered next to the project
// one, which is the workspace `gintrack snapshot` is meant for.
type teamHarness struct {
	*harness
	Team string
}

// newTeamHarness copies both fixtures and registers them.
func newTeamHarness(t *testing.T) *teamHarness {
	t.Helper()
	h := newHarness(t)
	h.register()
	team := filepath.Join(filepath.Dir(h.Repo), "acme-team")
	copyTree(t, filepath.Join("..", "..", "testdata", "fixtures", "team-basic"), team)
	if err := os.MkdirAll(filepath.Join(team, ".git"), 0o755); err != nil {
		t.Fatalf("create .git: %v", err)
	}
	if _, stderr, code := h.run("add", team, "--team"); code != exitOK {
		t.Fatalf("add --team: exit %d\n%s", code, stderr)
	}
	return &teamHarness{harness: h, Team: team}
}

// teamFile reads a file of the team repository.
func (h *teamHarness) teamFile(t *testing.T, rel string) (string, bool) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(h.Team, filepath.FromSlash(rel)))
	if err != nil {
		return "", false
	}
	return string(data), true
}

// snapshotOutput is the shape of `gintrack snapshot --json`.
type snapshotOutput struct {
	Team      string `json:"team"`
	DryRun    bool   `json:"dryRun"`
	Snapshots []struct {
		Project string `json:"project"`
		Path    string `json:"path"`
		Status  string `json:"status"`
		Items   int    `json:"items"`
		Reason  string `json:"reason"`
	} `json:"snapshots"`
	Written   int `json:"written"`
	Unchanged int `json:"unchanged"`
	Skipped   int `json:"skipped"`
}

// row returns the entry of a project key.
func (o snapshotOutput) row(t *testing.T, key string) struct {
	Project string `json:"project"`
	Path    string `json:"path"`
	Status  string `json:"status"`
	Items   int    `json:"items"`
	Reason  string `json:"reason"`
} {
	t.Helper()
	for _, r := range o.Snapshots {
		if r.Project == key {
			return r
		}
	}
	t.Fatalf("no entry for %s in %+v", key, o.Snapshots)
	return o.Snapshots[0]
}

func TestSnapshotCommand(t *testing.T) {
	h := newTeamHarness(t)

	t.Run("a dry run reports what would change and writes nothing", func(t *testing.T) {
		out := decode[snapshotOutput](t, h.mustRun("snapshot", "--dry-run", "--json"))
		if !out.DryRun || out.Written != 1 || out.Skipped != 1 {
			t.Fatalf("payload = %+v", out)
		}
		if row := out.row(t, "DEMO"); row.Status != "written" || row.Items == 0 {
			t.Fatalf("DEMO = %+v", row)
		}
		if _, found := h.teamFile(t, ".pmngr/index/DEMO.json"); found {
			t.Fatal("a dry run must not write the file")
		}
	})

	t.Run("the run writes a deterministic file for the cloned project", func(t *testing.T) {
		out := decode[snapshotOutput](t, h.mustRun("snapshot", "--json", "--generated-by", "jose"))
		if out.Written != 1 || out.Skipped != 1 {
			t.Fatalf("payload = %+v", out)
		}
		if row := out.row(t, "DEMO"); row.Path != ".pmngr/index/DEMO.json" {
			t.Fatalf("DEMO = %+v", row)
		}
		if row := out.row(t, "WEB"); row.Status != "skipped" || !strings.Contains(row.Reason, "not cloned") {
			t.Fatalf("WEB = %+v", row)
		}
		written, found := h.teamFile(t, ".pmngr/index/DEMO.json")
		if !found {
			t.Fatal("the snapshot was not written")
		}
		for _, want := range []string{`"key": "DEMO"`, `"generated_by": "jose"`, `"DEMO-US-0001"`, `"Guest checkout"`} {
			if !strings.Contains(written, want) {
				t.Errorf("the snapshot is missing %s:\n%s", want, written)
			}
		}
		if strings.Contains(written, "Northwind is the pilot customer") {
			t.Error("a body leaked into the snapshot (R-SNAP-1)")
		}
		if !strings.HasSuffix(written, "}\n") {
			t.Error("the snapshot must end with a trailing newline (R-SNAP-2)")
		}
	})

	t.Run("a second run leaves the file untouched", func(t *testing.T) {
		before, _ := h.teamFile(t, ".pmngr/index/DEMO.json")
		out := decode[snapshotOutput](t, h.mustRun("snapshot", "--json", "--generated-by", "someone-else"))
		if out.Written != 0 || out.Unchanged != 1 {
			t.Fatalf("payload = %+v", out)
		}
		after, _ := h.teamFile(t, ".pmngr/index/DEMO.json")
		if before != after {
			t.Error("a snapshot whose content did not change must not be rewritten")
		}
	})

	t.Run("a named project limits the run", func(t *testing.T) {
		out := decode[snapshotOutput](t, h.mustRun("snapshot", "WEB", "--json"))
		if len(out.Snapshots) != 1 || out.Snapshots[0].Project != "WEB" {
			t.Fatalf("payload = %+v", out)
		}
	})

	t.Run("the text output names the file and the counts", func(t *testing.T) {
		stdout := h.mustRun("snapshot")
		if !strings.Contains(stdout, ".pmngr/index/DEMO.json") || !strings.Contains(stdout, "unchanged") {
			t.Fatalf("stdout = %q", stdout)
		}
	})
}

func TestSnapshotCommandErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want int
	}{
		{name: "an unknown project key", args: []string{"snapshot", "NOPE"}, want: exitNotFound},
		{name: "a malformed project key", args: []string{"snapshot", "not-a-key"}, want: exitUsage},
		{name: "an unknown team repository", args: []string{"snapshot", "--team", "nope"}, want: exitNotFound},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newTeamHarness(t)
			_, stderr, code := h.run(tc.args...)
			if code != tc.want {
				t.Fatalf("exit %d, want %d\n%s", code, tc.want, stderr)
			}
		})
	}

	t.Run("no team repository is registered", func(t *testing.T) {
		h := newHarness(t)
		h.register()
		_, stderr, code := h.run("snapshot")
		if code != exitNotFound || !strings.Contains(stderr, "team repository") {
			t.Fatalf("exit %d\n%s", code, stderr)
		}
	})
}
