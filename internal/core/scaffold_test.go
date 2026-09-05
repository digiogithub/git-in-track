package core

import (
	"errors"
	"strings"
	"testing"
)

func TestCreateProject(t *testing.T) {
	tests := []struct {
		name    string
		seed    map[string]string
		docs    string
		spec    NewProject
		wantErr error
	}{
		{
			name: "in a documentation folder",
			docs: "docs",
			spec: NewProject{Key: "ACME", Name: "ACME Platform"},
		},
		{
			name: "at the repository root",
			docs: ".",
			spec: NewProject{Key: "ROOT"},
		},
		{
			name: "in a monorepo folder",
			docs: "apps/api/docs",
			spec: NewProject{Key: "API", Name: "API"},
		},
		{
			name:    "a key the grammar refuses",
			docs:    "docs",
			spec:    NewProject{Key: "acme"},
			wantErr: ErrProjectKey,
		},
		{
			name:    "a key that is too long",
			docs:    "docs",
			spec:    NewProject{Key: "ABCDEFGHIJK"},
			wantErr: ErrProjectKey,
		},
		{
			name:    "a folder that already holds a project",
			seed:    map[string]string{"docs/.pmngr/project.yaml": projectYAML("OLD")},
			docs:    "docs",
			spec:    NewProject{Key: "NEW"},
			wantErr: ErrProjectExists,
		},
		{
			name:    "a folder outside the repository",
			docs:    "../elsewhere",
			spec:    NewProject{Key: "NOPE"},
			wantErr: errNotSentinel,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fs := NewMemFSFromMap(tc.seed)
			ref, err := CreateProject(fs, tc.docs, tc.spec)
			if tc.wantErr != nil {
				if err == nil {
					t.Fatal("CreateProject: want an error, got none")
				}
				if !errors.Is(tc.wantErr, errNotSentinel) && !errors.Is(err, tc.wantErr) {
					t.Fatalf("CreateProject: error = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("CreateProject: %v", err)
			}

			data, readErr := fs.ReadFile(ref.ConfigPath)
			if readErr != nil {
				t.Fatalf("read %s: %v", ref.ConfigPath, readErr)
			}
			cfg, cfgErr := LoadProjectConfig(data)
			if cfgErr != nil {
				t.Fatalf("the written project.yaml does not validate: %v", cfgErr)
			}
			if cfg.Key != tc.spec.Key {
				t.Errorf("key = %q, want %q", cfg.Key, tc.spec.Key)
			}
			if cfg.InitialStatus() != "backlog" {
				t.Errorf("initial status = %q, want backlog", cfg.InitialStatus())
			}
			if !strings.Contains(string(data), "\n  statuses:") {
				t.Error("project.yaml is not emitted with two-space indentation")
			}

			// Every folder of docs/03 section 2 exists, and the derived index is
			// ignored (R-LOC-5).
			for _, dir := range []string{"epics", "stories", "tasks", "milestones", "comments", "attachments"} {
				if _, statErr := fs.Stat(ref.BacklogPath + "/" + dir); statErr != nil {
					t.Errorf("missing folder %s: %v", dir, statErr)
				}
			}
			ignore, ignoreErr := fs.ReadFile(ref.BacklogPath + "/.gitignore")
			if ignoreErr != nil || !strings.Contains(string(ignore), "index.json") {
				t.Errorf(".gitignore = %q, %v", ignore, ignoreErr)
			}

			// The project the scaffolder wrote is the project discovery finds.
			found, discoverErr := DiscoverProjectsWith(fs, DiscoveryOptions{DocsFolders: []string{ref.DocsPath}})
			if discoverErr != nil {
				t.Fatalf("DiscoverProjectsWith: %v", discoverErr)
			}
			if len(found) != 1 || found[0].Key != tc.spec.Key {
				t.Fatalf("discovery found %+v, want one %s", found, tc.spec.Key)
			}
		})
	}
}

// errNotSentinel marks a case that only asserts that some error was returned.
var errNotSentinel = errors.New("any error")

func TestNewProjectConfigDefaults(t *testing.T) {
	tests := []struct {
		name     string
		spec     NewProject
		docs     string
		wantName string
		wantTZ   string
		wantPath string
	}{
		{name: "the key stands in for a missing name", spec: NewProject{Key: "ACME"}, docs: "docs", wantName: "ACME", wantTZ: "UTC", wantPath: "docs"},
		{name: "the root has no docs path", spec: NewProject{Key: "ACME", Name: "Platform"}, docs: ".", wantName: "Platform", wantTZ: "UTC"},
		{name: "an explicit timezone wins", spec: NewProject{Key: "ACME", Timezone: "Europe/Madrid"}, docs: "docs", wantName: "ACME", wantTZ: "Europe/Madrid", wantPath: "docs"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := NewProjectConfig(tc.spec, tc.docs)
			if cfg.Name != tc.wantName {
				t.Errorf("name = %q, want %q", cfg.Name, tc.wantName)
			}
			if cfg.Timezone != tc.wantTZ {
				t.Errorf("timezone = %q, want %q", cfg.Timezone, tc.wantTZ)
			}
			if cfg.Docs.Path != tc.wantPath {
				t.Errorf("docs.path = %q, want %q", cfg.Docs.Path, tc.wantPath)
			}
			if cfg.Schema != SupportedSchema {
				t.Errorf("schema = %d, want %d", cfg.Schema, SupportedSchema)
			}
			if diags := cfg.Validate(); len(diags) != 0 {
				t.Errorf("a fresh configuration must validate cleanly, got %v", diags)
			}
		})
	}
}
