package core

import (
	"errors"
	"strings"
	"testing"
)

// teamWithProjects builds a configuration holding the given project keys, each
// with the fields R-PROJ-1 requires.
func teamWithProjects(keys ...ProjectKey) *TeamConfig {
	cfg := NewTeamConfig(NewTeam{Key: "ACME-TEAM", Name: "ACME Delivery Team"})
	for _, key := range keys {
		cfg.Projects = append(cfg.Projects, TeamProject{
			Key:      key,
			Name:     string(key) + " Platform",
			Repo:     "https://github.com/acme/" + strings.ToLower(string(key)) + ".git",
			DocsPath: "docs",
		})
	}
	return &cfg
}

func TestAddTeamProject(t *testing.T) {
	valid := TeamProject{
		Key: "WEB", Name: "Web", Repo: "https://github.com/acme/web.git", DocsPath: "docs",
	}

	tests := []struct {
		name    string
		start   []ProjectKey
		entry   TeamProject
		wantErr error
		want    []ProjectKey
	}{
		{
			name:  "adds the first project",
			entry: valid,
			want:  []ProjectKey{"WEB"},
		},
		{
			name:  "inserts in key order rather than appending",
			start: []ProjectKey{"ACME", "ZOO"},
			entry: valid,
			want:  []ProjectKey{"ACME", "WEB", "ZOO"},
		},
		{
			name:  "appends a key that sorts last",
			start: []ProjectKey{"ACME", "TOOLS"},
			entry: valid,
			want:  []ProjectKey{"ACME", "TOOLS", "WEB"},
		},
		{
			name:    "refuses a duplicate key",
			start:   []ProjectKey{"WEB"},
			entry:   valid,
			wantErr: ErrTeamProjectExists,
			want:    []ProjectKey{"WEB"},
		},
		{
			name:    "refuses a key outside the grammar",
			entry:   TeamProject{Key: "web", Name: "Web", Repo: "x", DocsPath: "docs"},
			wantErr: ErrTeamProjectFields,
		},
		{
			name:    "refuses an entry with no repository",
			entry:   TeamProject{Key: "WEB", Name: "Web", DocsPath: "docs"},
			wantErr: ErrTeamProjectFields,
		},
		{
			name:    "refuses an entry with no docs path",
			entry:   TeamProject{Key: "WEB", Name: "Web", Repo: "https://x/y.git"},
			wantErr: ErrTeamProjectFields,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := teamWithProjects(tt.start...)
			added, err := AddTeamProject(cfg, tt.entry)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("error = %v, want %v", err, tt.wantErr)
				}
			} else {
				if err != nil {
					t.Fatalf("AddTeamProject: %v", err)
				}
				if added.Key != tt.entry.Key {
					t.Errorf("added key = %q, want %q", added.Key, tt.entry.Key)
				}
			}
			if tt.want == nil {
				return
			}
			var got []ProjectKey
			for _, p := range cfg.Projects {
				got = append(got, p.Key)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("projects = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("projects = %v, want %v", got, tt.want)
				}
			}
		})
	}

	t.Run("defaults the name to the key and cleans the docs path", func(t *testing.T) {
		cfg := teamWithProjects()
		added, err := AddTeamProject(cfg, TeamProject{
			Key: "WEB", Repo: " https://github.com/acme/web.git ", DocsPath: "./docs/",
			WebURL: "https://github.com/acme/web/",
		})
		if err != nil {
			t.Fatalf("AddTeamProject: %v", err)
		}
		if added.Name != "WEB" {
			t.Errorf("name = %q, want the key", added.Name)
		}
		if added.DocsPath != "docs" {
			t.Errorf("docs path = %q, want docs", added.DocsPath)
		}
		if added.Repo != "https://github.com/acme/web.git" {
			t.Errorf("repo = %q, want it trimmed", added.Repo)
		}
		if added.WebURL != "https://github.com/acme/web" {
			t.Errorf("web url = %q, want no trailing slash", added.WebURL)
		}
	})
}

func TestRemoveTeamProject(t *testing.T) {
	t.Run("drops the entry and keeps every other one in place", func(t *testing.T) {
		cfg := teamWithProjects("ACME", "TOOLS", "WEB")
		removed, err := RemoveTeamProject(cfg, "TOOLS")
		if err != nil {
			t.Fatalf("RemoveTeamProject: %v", err)
		}
		if removed.Key != "TOOLS" {
			t.Errorf("removed = %q", removed.Key)
		}
		if len(cfg.Projects) != 2 || cfg.Projects[0].Key != "ACME" || cfg.Projects[1].Key != "WEB" {
			t.Errorf("projects = %+v", cfg.Projects)
		}
	})

	t.Run("refuses a project the team does not declare", func(t *testing.T) {
		cfg := teamWithProjects("ACME")
		if _, err := RemoveTeamProject(cfg, "WEB"); !errors.Is(err, ErrTeamProjectUnknown) {
			t.Fatalf("error = %v, want ErrTeamProjectUnknown", err)
		}
	})
}

func TestTeamProjectReferences(t *testing.T) {
	board := &Board{ID: "delivery", Path: ".pmngr/boards/delivery.md", Projects: []ProjectKey{"ACME", "WEB"}}
	board.OrderList().Set("doing", []string{"WEB/WEB-US-0001", "ACME/ACME-US-0009"})
	sprint := &Sprint{
		ID: "ACME-TEAM-S-0001", Path: ".pmngr/sprints/ACME-TEAM-S-0001.md",
		Items:     []string{"WEB/WEB-US-0001", "ACME/ACME-T-0002"},
		Committed: []string{"WEB/WEB-US-0002"},
	}
	retro := &Retro{
		ID: "ACME-TEAM-R-0001", Path: ".pmngr/retros/ACME-TEAM-R-0001-sprint-1.md",
		Actions: []RetroAction{{ID: "a1", Task: "WEB/WEB-T-0007"}, {ID: "a2"}},
	}
	artifacts := TeamArtifacts{Boards: []*Board{board}, Sprints: []*Sprint{sprint}, Retros: []*Retro{retro}}

	tests := []struct {
		name  string
		key   ProjectKey
		want  int
		kinds []string
	}{
		{name: "every artifact kind reports its references", key: "WEB", want: 5,
			kinds: []string{"board", "board", "retro", "sprint", "sprint"}},
		{name: "a project only a board and a sprint name", key: "ACME", want: 3,
			kinds: []string{"board", "board", "sprint"}},
		{name: "a project nothing references", key: "TOOLS", want: 0},
		{name: "an empty key matches nothing", key: "", want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TeamProjectReferences(tt.key, artifacts)
			if len(got) != tt.want {
				t.Fatalf("references = %+v, want %d", got, tt.want)
			}
			for i, kind := range tt.kinds {
				if got[i].Kind != kind {
					t.Errorf("reference %d kind = %q, want %q (%+v)", i, got[i].Kind, kind, got)
				}
			}
		})
	}

	t.Run("nil artifacts are simply empty", func(t *testing.T) {
		if got := TeamProjectReferences("WEB", TeamArtifacts{Boards: []*Board{nil}}); len(got) != 0 {
			t.Fatalf("references = %+v", got)
		}
	})
}

func TestUpdateTeamProjects(t *testing.T) {
	newTeamFS := func(t *testing.T) FS {
		t.Helper()
		fs := NewMemFS()
		if _, err := CreateTeam(fs, ".", NewTeam{Key: "ACME-TEAM", Name: "ACME Delivery Team"}); err != nil {
			t.Fatalf("CreateTeam: %v", err)
		}
		return fs
	}

	t.Run("writes an added project back through the single emitter", func(t *testing.T) {
		fs := newTeamFS(t)
		err := UpdateTeamProjects(fs, ".", func(cfg *TeamConfig) error {
			_, addErr := AddTeamProject(cfg, TeamProject{
				Key: "ACME", Name: "ACME Platform",
				Repo: "https://github.com/acme/platform.git", DocsPath: "docs",
			})
			return addErr
		})
		if err != nil {
			t.Fatalf("UpdateTeamProjects: %v", err)
		}
		data, err := fs.ReadFile("team.yaml")
		if err != nil {
			t.Fatalf("read team.yaml: %v", err)
		}
		cfg, err := LoadTeamConfig(data)
		if err != nil {
			t.Fatalf("LoadTeamConfig refuses what was written: %v", err)
		}
		if _, ok := cfg.Project("ACME"); !ok {
			t.Fatalf("the project is not in the file:\n%s", data)
		}
		again, err := MarshalTeamConfig(*cfg)
		if err != nil {
			t.Fatalf("MarshalTeamConfig: %v", err)
		}
		if string(again) != string(data) {
			t.Errorf("the round trip is not byte-stable:\n%s\n---\n%s", data, again)
		}
	})

	t.Run("a removal leaves the rest of the file untouched", func(t *testing.T) {
		fs := newTeamFS(t)
		for _, key := range []ProjectKey{"ACME", "WEB"} {
			if err := UpdateTeamProjects(fs, ".", func(cfg *TeamConfig) error {
				_, addErr := AddTeamProject(cfg, TeamProject{
					Key: key, Name: string(key), Repo: "https://github.com/acme/x.git", DocsPath: "docs",
				})
				return addErr
			}); err != nil {
				t.Fatalf("add %s: %v", key, err)
			}
		}
		before, err := fs.ReadFile("team.yaml")
		if err != nil {
			t.Fatalf("read team.yaml: %v", err)
		}
		if err := UpdateTeamProjects(fs, ".", func(cfg *TeamConfig) error {
			_, removeErr := RemoveTeamProject(cfg, "WEB")
			return removeErr
		}); err != nil {
			t.Fatalf("remove: %v", err)
		}
		after, err := fs.ReadFile("team.yaml")
		if err != nil {
			t.Fatalf("read team.yaml: %v", err)
		}
		if !strings.Contains(string(before), "key: WEB") {
			t.Fatalf("the fixture never held WEB:\n%s", before)
		}
		if strings.Contains(string(after), "key: WEB") {
			t.Errorf("WEB survived the removal:\n%s", after)
		}
		if !strings.Contains(string(after), "key: ACME") {
			t.Errorf("the removal took ACME with it:\n%s", after)
		}
	})

	t.Run("a folder with no team.yaml is refused", func(t *testing.T) {
		err := UpdateTeamProjects(NewMemFS(), ".", func(*TeamConfig) error { return nil })
		if !errors.Is(err, ErrTeamMissing) {
			t.Fatalf("error = %v, want ErrTeamMissing", err)
		}
	})

	t.Run("the mutation error is returned and nothing is written", func(t *testing.T) {
		fs := newTeamFS(t)
		before, err := fs.ReadFile("team.yaml")
		if err != nil {
			t.Fatalf("read team.yaml: %v", err)
		}
		err = UpdateTeamProjects(fs, ".", func(cfg *TeamConfig) error {
			_, removeErr := RemoveTeamProject(cfg, "NOPE")
			return removeErr
		})
		if !errors.Is(err, ErrTeamProjectUnknown) {
			t.Fatalf("error = %v, want ErrTeamProjectUnknown", err)
		}
		after, err := fs.ReadFile("team.yaml")
		if err != nil {
			t.Fatalf("read team.yaml: %v", err)
		}
		if string(after) != string(before) {
			t.Errorf("the file changed despite the failure")
		}
	})
}
