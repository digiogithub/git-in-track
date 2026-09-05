package vault

import (
	"testing"

	"github.com/digiogithub/git-in-track/internal/core"
)

// emptyVault returns a vault over a repository that holds no backlog at all.
func emptyVault(t *testing.T, seed map[string]string) *Vault {
	t.Helper()
	v, err := Open(core.NewMemFSFromMap(seed), "repo")
	if err != nil {
		t.Fatalf("open vault: %v", err)
	}
	return v
}

func TestProjectCreate(t *testing.T) {
	t.Run("scaffolds a backlog and indexes it", func(t *testing.T) {
		v := emptyVault(t, map[string]string{"README.md": "# repo\n"})
		if got := len(v.Projects()); got != 0 {
			t.Fatalf("a fresh repository reports %d projects, want 0", got)
		}

		result := decode[projectCreated](t, call(t, v, "project.create", map[string]any{
			"docsFolder": "docs",
			"key":        "ACME",
			"name":       "ACME Platform",
		}))

		if result.Project.Key != "ACME" || result.Project.Name != "ACME Platform" {
			t.Errorf("project = %+v", result.Project)
		}
		if !result.Project.Writable {
			t.Error("a freshly created project must be writable")
		}
		if result.Project.BacklogPath != "docs/.pmngr" {
			t.Errorf("backlog path = %q", result.Project.BacklogPath)
		}
		var wrote bool
		for _, f := range result.Writes.Written {
			if f.Path == "docs/.pmngr/project.yaml" {
				wrote = true
			}
		}
		if !wrote {
			t.Errorf("the write set does not carry project.yaml: %+v", result.Writes.Written)
		}
		if got := len(v.Projects()); got != 1 {
			t.Errorf("the vault reports %d projects after creating one, want 1", got)
		}

		// The new project is immediately writable through the normal contract.
		call(t, v, "item.create", map[string]any{
			"project": "ACME",
			"type":    "story",
			"title":   "First story",
		})
	})

	t.Run("a deep folder stays discoverable after a reload", func(t *testing.T) {
		v := emptyVault(t, map[string]string{"apps/api/README.md": "# api\n"})
		call(t, v, "project.create", map[string]any{"docsFolder": "apps/api/docs", "key": "API"})
		if _, err := v.Reload(t.Context()); err != nil {
			t.Fatalf("reload: %v", err)
		}
		projects := v.Projects()
		if len(projects) != 1 || projects[0].Key != "API" {
			t.Fatalf("projects after reload = %+v, want API", projects)
		}
	})

	t.Run("refusals carry the stable codes", func(t *testing.T) {
		tests := []struct {
			name   string
			params map[string]any
			want   string
		}{
			{name: "an invalid key", params: map[string]any{"docsFolder": "docs", "key": "acme"}, want: "validation_failed"},
			{name: "no key at all", params: map[string]any{"docsFolder": "docs"}, want: "validation_failed"},
			{name: "a folder outside the repository", params: map[string]any{"docsFolder": "../x", "key": "ACME"}, want: "internal"},
		}
		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				v := emptyVault(t, map[string]string{"README.md": "# repo\n"})
				env := rawCall(t, v, "project.create", tc.params)
				if env.OK {
					t.Fatalf("project.create accepted %+v", tc.params)
				}
				if env.Error.Code != tc.want {
					t.Errorf("code = %q, want %q (%s)", env.Error.Code, tc.want, env.Error.Message)
				}
			})
		}
	})

	t.Run("an existing project is never overwritten", func(t *testing.T) {
		v := emptyVault(t, map[string]string{"README.md": "# repo\n"})
		call(t, v, "project.create", map[string]any{"docsFolder": "docs", "key": "ACME"})
		env := rawCall(t, v, "project.create", map[string]any{"docsFolder": "docs", "key": "OTHER"})
		if env.OK || env.Error.Code != "project_exists" {
			t.Fatalf("second create = ok:%v code:%q", env.OK, env.Error.Code)
		}
	})
}

func TestDeclaredDocsFoldersAreDiscovered(t *testing.T) {
	files := map[string]string{
		"apps/api/docs/.pmngr/project.yaml": "schema: 1\nkey: API\nworkflow:\n  statuses:\n    - {id: todo, category: todo}\n    - {id: done, category: done}\n",
	}
	tests := []struct {
		name    string
		declare []string
		want    int
	}{
		{name: "undeclared, so out of reach", declare: nil, want: 0},
		{name: "declared, so found at any depth", declare: []string{"apps/api/docs"}, want: 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			v, err := OpenWithDocs(core.NewMemFSFromMap(files), "repo", tc.declare)
			if err != nil {
				t.Fatalf("open vault: %v", err)
			}
			if got := len(v.Projects()); got != tc.want {
				t.Errorf("projects = %d, want %d", got, tc.want)
			}
		})
	}
}
