package core

import (
	"reflect"
	"testing"
)

// project returns the smallest valid project.yaml for a key.
func projectYAML(key string) string {
	return "schema: 1\nkey: " + key +
		"\nworkflow:\n  statuses:\n    - {id: todo, category: todo}\n    - {id: done, category: done}\n"
}

func TestDiscoverProjectsIsBounded(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		opts  DiscoveryOptions
		want  []string
	}{
		{
			name:  "the repository root itself",
			files: map[string]string{".pmngr/project.yaml": projectYAML("ROOT")},
			want:  []string{"ROOT"},
		},
		{
			name:  "a first-level documentation folder",
			files: map[string]string{"docs/.pmngr/project.yaml": projectYAML("DOCS")},
			want:  []string{"DOCS"},
		},
		{
			name: "test fixtures two levels down are not projects",
			files: map[string]string{
				"docs/.pmngr/project.yaml":                     projectYAML("GIT"),
				"testdata/basic/docs/.pmngr/project.yaml":      projectYAML("DEMO"),
				"internal/core/testdata/x/.pmngr/project.yaml": projectYAML("ACME"),
			},
			want: []string{"GIT"},
		},
		{
			name: "a declared monorepo folder is found at any depth",
			files: map[string]string{
				"apps/api/docs/.pmngr/project.yaml": projectYAML("API"),
				"apps/web/docs/.pmngr/project.yaml": projectYAML("WEB"),
			},
			opts: DiscoveryOptions{DocsFolders: []string{"apps/api/docs", "apps/web/docs"}},
			want: []string{"API", "WEB"},
		},
		{
			name: "an undeclared monorepo folder stays invisible",
			files: map[string]string{
				"apps/api/docs/.pmngr/project.yaml": projectYAML("API"),
			},
			want: nil,
		},
		{
			name: "a declared folder that holds nothing is not an error",
			files: map[string]string{
				"docs/.pmngr/project.yaml": projectYAML("DOCS"),
			},
			opts: DiscoveryOptions{DocsFolders: []string{"nowhere/at/all"}},
			want: []string{"DOCS"},
		},
		{
			name: "a declared folder never escapes the vault",
			files: map[string]string{
				"docs/.pmngr/project.yaml": projectYAML("DOCS"),
			},
			opts: DiscoveryOptions{DocsFolders: []string{"../elsewhere", "/etc"}},
			want: []string{"DOCS"},
		},
		{
			name: "declaring a folder the rule already reaches reports it once",
			files: map[string]string{
				"docs/.pmngr/project.yaml": projectYAML("DOCS"),
			},
			opts: DiscoveryOptions{DocsFolders: []string{"docs", "./docs"}},
			want: []string{"DOCS"},
		},
		{
			name: "each root gets its own shallow rule",
			files: map[string]string{
				"alpha/docs/.pmngr/project.yaml": projectYAML("ALPHA"),
				"beta/docs/.pmngr/project.yaml":  projectYAML("BETA"),
			},
			opts: DiscoveryOptions{Roots: []string{"alpha", "beta"}},
			want: []string{"ALPHA", "BETA"},
		},
		{
			name: "noise directories are never probed",
			files: map[string]string{
				"node_modules/pkg/.pmngr/project.yaml": projectYAML("NOPE"),
				"vendor/.pmngr/project.yaml":           projectYAML("VENDOR"),
				"docs/.pmngr/project.yaml":             projectYAML("OK"),
			},
			want: []string{"OK"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fs := NewMemFSFromMap(tc.files)
			found, err := DiscoverProjectsWith(fs, tc.opts)
			if err != nil {
				t.Fatalf("DiscoverProjectsWith: %v", err)
			}
			var keys []string
			for _, ref := range found {
				keys = append(keys, string(ref.Key))
			}
			if !reflect.DeepEqual(keys, tc.want) {
				t.Errorf("keys = %v, want %v", keys, tc.want)
			}
		})
	}
}
