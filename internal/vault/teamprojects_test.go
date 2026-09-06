package vault

import (
	"strings"
	"testing"

	"github.com/digiogithub/git-in-track/internal/core"
)

// newProject is the entry the tests connect to the fixture team.
func newProject() map[string]any {
	return map[string]any{
		"key":           "TOOLS",
		"name":          "Internal Tools",
		"repo":          "https://github.com/example/tools.git",
		"defaultBranch": "trunk",
		"docsPath":      "docs",
	}
}

func TestWorkspaceTeamProjectAdd(t *testing.T) {
	cases := []struct {
		name  string
		entry map[string]any
		check func(t *testing.T, w *Workspace, result TeamProjectResult)
	}{
		{
			name:  "declares the project and writes team.yaml",
			entry: newProject(),
			check: func(t *testing.T, w *Workspace, result TeamProjectResult) {
				if result.Project.Key != "TOOLS" || result.Project.Branch() != "trunk" {
					t.Fatalf("project = %+v", result.Project)
				}
				if len(result.Writes) != 1 || len(result.Writes[0].Written) != 1 {
					t.Fatalf("writes = %+v", result.Writes)
				}
				if got := result.Writes[0].Written[0].Path; got != core.TeamFileName {
					t.Fatalf("written path = %q, want %s", got, core.TeamFileName)
				}
				if len(result.Team.Projects) != 3 {
					t.Fatalf("projects = %d, want 3", len(result.Team.Projects))
				}
				// The list is sorted by key, so a second machine adding another
				// project writes a hunk somewhere else in the file.
				var keys []string
				for _, p := range result.Team.Projects {
					keys = append(keys, string(p.Key))
				}
				if strings.Join(keys, ",") != "DEMO,TOOLS,WEB" {
					t.Fatalf("keys = %v, want DEMO,TOOLS,WEB", keys)
				}
				// The change is visible through the ordinary read path.
				summary := decode[teamSummary](t, wsCall(t, w, "team.get", nil))
				if len(summary.Projects) != 3 {
					t.Fatalf("team.get projects = %d, want 3", len(summary.Projects))
				}
			},
		},
		{
			name: "an entry with no name takes the key",
			entry: map[string]any{
				"key": "TOOLS", "repo": "https://github.com/example/tools.git", "docsPath": "docs",
			},
			check: func(t *testing.T, _ *Workspace, result TeamProjectResult) {
				if result.Project.Name != "TOOLS" {
					t.Fatalf("name = %q, want the key", result.Project.Name)
				}
			},
		},
		{
			// The link between a registered repository and a team.yaml entry is
			// the project key alone: same remote, different key, not cloned.
			name: "an entry is cloned only when an open repository serves its key",
			entry: map[string]any{
				"key": "DEMO2", "repo": "https://github.com/example/demo-shop.git", "docsPath": "docs",
			},
			check: func(t *testing.T, _ *Workspace, result TeamProjectResult) {
				for _, p := range result.Team.Projects {
					if p.Key == "DEMO2" && p.Cloned {
						t.Fatal("no open repository serves DEMO2; it cannot be cloned")
					}
					if p.Key == "DEMO" && !p.Cloned {
						t.Fatal("DEMO is open in this workspace and must stay cloned")
					}
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			writableModes(t, func(t *testing.T, w *Workspace) {
				result := decode[TeamProjectResult](t, wsCall(t, w, "team.project.add",
					map[string]any{"project": tc.entry}))
				tc.check(t, w, result)
			})
		})
	}
}

func TestWorkspaceTeamProjectAddRefusals(t *testing.T) {
	cases := []struct {
		name  string
		entry map[string]any
		code  string
	}{
		{"a key the team already declares", map[string]any{
			"key": "DEMO", "name": "Demo again",
			"repo": "https://github.com/example/other.git", "docsPath": "docs",
		}, TeamProjectExistsCode},
		{"a key outside the grammar", map[string]any{
			"key": "demo-shop", "repo": "https://github.com/example/x.git", "docsPath": "docs",
		}, "validation_failed"},
		{"no repository URL", map[string]any{"key": "TOOLS", "docsPath": "docs"}, "validation_failed"},
		{"no docs path", map[string]any{
			"key": "TOOLS", "repo": "https://github.com/example/x.git",
		}, "validation_failed"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			writableModes(t, func(t *testing.T, w *Workspace) {
				code, message := wsFail(t, w, "team.project.add", map[string]any{"project": tc.entry})
				if code != tc.code {
					t.Fatalf("code = %q (%s), want %q", code, message, tc.code)
				}
				// Nothing was written: the team still declares its two projects.
				summary := decode[teamSummary](t, wsCall(t, w, "team.get", nil))
				if len(summary.Projects) != 2 {
					t.Fatalf("projects = %d, want the fixture's 2", len(summary.Projects))
				}
			})
		})
	}
}

func TestWorkspaceTeamProjectRemove(t *testing.T) {
	t.Run("a project nothing references", func(t *testing.T) {
		writableModes(t, func(t *testing.T, w *Workspace) {
			wsCall(t, w, "team.project.add", map[string]any{"project": newProject()})
			result := decode[TeamProjectResult](t, wsCall(t, w, "team.project.remove",
				map[string]any{"key": "TOOLS"}))
			if result.Project.Key != "TOOLS" {
				t.Fatalf("removed = %+v", result.Project)
			}
			if len(result.References) != 0 {
				t.Fatalf("references = %+v, want none", result.References)
			}
			if len(result.Team.Projects) != 2 {
				t.Fatalf("projects = %d, want 2", len(result.Team.Projects))
			}
		})
	})

	t.Run("a referenced project is refused and then forced", func(t *testing.T) {
		writableModes(t, func(t *testing.T, w *Workspace) {
			code, message := wsFail(t, w, "team.project.remove", map[string]any{"key": "WEB"})
			if code != TeamProjectReferencedCode {
				t.Fatalf("code = %q (%s), want %q", code, message, TeamProjectReferencedCode)
			}
			// The message names what breaks rather than only counting it.
			for _, want := range []string{"WEB", "board", "sprint"} {
				if !strings.Contains(message, want) {
					t.Errorf("message %q does not mention %q", message, want)
				}
			}
			summary := decode[teamSummary](t, wsCall(t, w, "team.get", nil))
			if len(summary.Projects) != 2 {
				t.Fatalf("the refusal wrote anyway: projects = %d", len(summary.Projects))
			}

			result := decode[TeamProjectResult](t, wsCall(t, w, "team.project.remove",
				map[string]any{"key": "WEB", "force": true}))
			if len(result.References) == 0 {
				t.Error("a forced removal must report what it broke")
			}
			if len(result.Team.Projects) != 1 || result.Team.Projects[0].Key != "DEMO" {
				t.Fatalf("projects = %+v", result.Team.Projects)
			}
		})
	})

	t.Run("refusals carry the stable codes", func(t *testing.T) {
		cases := []struct {
			name   string
			params map[string]any
			code   string
		}{
			{"a project the team does not declare", map[string]any{"key": "NOPE"}, "not_found"},
			{"no key at all", map[string]any{}, "invalid_request"},
			{"an unknown team", map[string]any{"key": "DEMO", "team": "OTHER"}, "not_found"},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				writableModes(t, func(t *testing.T, w *Workspace) {
					code, message := wsFail(t, w, "team.project.remove", tc.params)
					if code != tc.code {
						t.Fatalf("code = %q (%s), want %q", code, message, tc.code)
					}
				})
			})
		}
	})
}

func TestWorkspaceTeamProjectScope(t *testing.T) {
	t.Run("the team is named by key or by repository id", func(t *testing.T) {
		writableModes(t, func(t *testing.T, w *Workspace) {
			wsCall(t, w, "team.project.add", map[string]any{
				"team": "DEMO-TEAM", "project": newProject(),
			})
			result := decode[TeamProjectResult](t, wsCall(t, w, "team.project.remove",
				map[string]any{"team": "demo-team", "key": "TOOLS"}))
			if result.Project.Key != "TOOLS" {
				t.Fatalf("removed = %+v", result.Project)
			}
		})
	})

	t.Run("a repository that is not a team repository is not one", func(t *testing.T) {
		writableModes(t, func(t *testing.T, w *Workspace) {
			code, _ := wsFail(t, w, "team.project.add", map[string]any{
				"team": "demo", "project": newProject(),
			})
			if code != "not_found" {
				t.Fatalf("code = %q, want not_found", code)
			}
		})
	})
}
