package core

import (
	"errors"
	"strings"
	"testing"
)

func TestCreateTeam(t *testing.T) {
	t.Run("writes team.yaml and the team layout", func(t *testing.T) {
		fs := NewMemFSFromMap(map[string]string{"README.md": "# team\n"})
		ref, err := CreateTeam(fs, ".", NewTeam{Key: "ACME-TEAM", Name: "ACME Delivery Team"})
		if err != nil {
			t.Fatalf("CreateTeam: %v", err)
		}
		if ref.Key != "ACME-TEAM" || ref.Name != "ACME Delivery Team" {
			t.Errorf("ref = %+v", ref)
		}
		if ref.ConfigPath != "team.yaml" {
			t.Errorf("config path = %q, want team.yaml", ref.ConfigPath)
		}
		if ref.KnowledgePath != "knowledge" {
			t.Errorf("knowledge path = %q, want knowledge", ref.KnowledgePath)
		}
		for _, dir := range []string{".pmngr/boards", ".pmngr/sprints", ".pmngr/retros", ".pmngr/index", "knowledge"} {
			info, statErr := fs.Stat(dir)
			if statErr != nil || !info.IsDir {
				t.Errorf("%s is not a directory: %v", dir, statErr)
			}
		}
		if _, readErr := fs.ReadFile("knowledge/index.md"); readErr != nil {
			t.Errorf("knowledge/index.md: %v", readErr)
		}
	})

	t.Run("a fresh team repository loads without an error diagnostic", func(t *testing.T) {
		fs := NewMemFS()
		ref, err := CreateTeam(fs, ".", NewTeam{Key: "ACME-TEAM"})
		if err != nil {
			t.Fatalf("CreateTeam: %v", err)
		}
		for _, d := range ref.Diagnostics {
			if d.Severity == SeverityError {
				t.Errorf("fresh team repository reports an error: %+v", d)
			}
		}
		data, err := fs.ReadFile("team.yaml")
		if err != nil {
			t.Fatalf("read team.yaml: %v", err)
		}
		if _, err := LoadTeamConfig(data); err != nil {
			t.Fatalf("LoadTeamConfig refuses the file the scaffolder wrote: %v", err)
		}
		// The name defaults to the key, exactly like a project's.
		if ref.Name != "ACME-TEAM" {
			t.Errorf("name = %q, want the key", ref.Name)
		}
	})

	t.Run("the emitted file round-trips byte for byte", func(t *testing.T) {
		fs := NewMemFS()
		if _, err := CreateTeam(fs, ".", NewTeam{
			Key:         "ACME-TEAM",
			Name:        "ACME Delivery Team",
			Description: "Squad owning the platform.",
			Timezone:    "Europe/Madrid",
			Members: []Member{
				{Handle: "jose", Name: "Jose Ruiz", Role: "lead", Emails: []string{"jose@example.com"}, Active: true},
			},
		}); err != nil {
			t.Fatalf("CreateTeam: %v", err)
		}
		first, err := fs.ReadFile("team.yaml")
		if err != nil {
			t.Fatalf("read team.yaml: %v", err)
		}
		cfg, err := LoadTeamConfig(first)
		if err != nil {
			t.Fatalf("LoadTeamConfig: %v", err)
		}
		second, err := MarshalTeamConfig(*cfg)
		if err != nil {
			t.Fatalf("MarshalTeamConfig: %v", err)
		}
		if string(first) != string(second) {
			t.Errorf("team.yaml does not round-trip:\n--- written\n%s\n--- re-emitted\n%s", first, second)
		}
	})

	t.Run("a team repository is never overwritten", func(t *testing.T) {
		fs := NewMemFSFromMap(map[string]string{"team.yaml": "schema: 1\nkey: OLD\n"})
		_, err := CreateTeam(fs, ".", NewTeam{Key: "NEW"})
		if !errors.Is(err, ErrTeamExists) {
			t.Fatalf("err = %v, want ErrTeamExists", err)
		}
		data, _ := fs.ReadFile("team.yaml")
		if !strings.Contains(string(data), "key: OLD") {
			t.Errorf("the existing file was rewritten: %s", data)
		}
	})

	t.Run("refusals", func(t *testing.T) {
		tests := []struct {
			name string
			root string
			spec NewTeam
			want error
		}{
			{name: "a lowercase key", spec: NewTeam{Key: "acme"}, want: ErrTeamKey},
			{name: "no key at all", spec: NewTeam{}, want: ErrTeamKey},
			{name: "a one-character key", spec: NewTeam{Key: "A"}, want: ErrTeamKey},
			{
				name: "a malformed member handle",
				spec: NewTeam{Key: "ACME", Members: []Member{{Handle: "Jose"}}},
				want: ErrMemberHandle,
			},
			{
				name: "a member declared twice",
				spec: NewTeam{Key: "ACME", Members: []Member{{Handle: "jose"}, {Handle: "jose"}}},
				want: ErrMemberHandle,
			},
		}
		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				fs := NewMemFS()
				_, err := CreateTeam(fs, tc.root, tc.spec)
				if !errors.Is(err, tc.want) {
					t.Fatalf("err = %v, want %v", err, tc.want)
				}
				if _, statErr := fs.Stat("team.yaml"); statErr == nil {
					t.Error("a refused creation still wrote team.yaml")
				}
			})
		}
	})

	t.Run("a folder outside the repository is refused", func(t *testing.T) {
		if _, err := CreateTeam(NewMemFS(), "../elsewhere", NewTeam{Key: "ACME"}); err == nil {
			t.Fatal("CreateTeam accepted a root outside the repository")
		}
	})

	t.Run("a nested root carries the whole layout", func(t *testing.T) {
		fs := NewMemFS()
		ref, err := CreateTeam(fs, "teams/acme", NewTeam{Key: "ACME", KnowledgePath: "wiki"})
		if err != nil {
			t.Fatalf("CreateTeam: %v", err)
		}
		if ref.ConfigPath != "teams/acme/team.yaml" {
			t.Errorf("config path = %q", ref.ConfigPath)
		}
		if ref.KnowledgePath != "teams/acme/wiki" {
			t.Errorf("knowledge path = %q", ref.KnowledgePath)
		}
		if _, err := fs.ReadFile("teams/acme/wiki/index.md"); err != nil {
			t.Errorf("the knowledge index was not written: %v", err)
		}
	})
}

func TestNewTeamConfigDefaults(t *testing.T) {
	tests := []struct {
		name string
		spec NewTeam
		want func(TeamConfig) error
	}{
		{
			name: "the name defaults to the key",
			spec: NewTeam{Key: "ACME"},
			want: func(cfg TeamConfig) error {
				if cfg.Name != "ACME" {
					return errors.New("name is not the key")
				}
				return nil
			},
		},
		{
			name: "the timezone defaults to UTC",
			spec: NewTeam{Key: "ACME"},
			want: func(cfg TeamConfig) error {
				if cfg.Timezone != "UTC" {
					return errors.New("timezone is not UTC")
				}
				return nil
			},
		},
		{
			name: "no project is declared",
			spec: NewTeam{Key: "ACME"},
			want: func(cfg TeamConfig) error {
				if len(cfg.Projects) != 0 {
					return errors.New("a project was invented")
				}
				return nil
			},
		},
		{
			name: "the schema is the supported one",
			spec: NewTeam{Key: "ACME"},
			want: func(cfg TeamConfig) error {
				if cfg.Schema != SupportedTeamSchema {
					return errors.New("schema is not the supported one")
				}
				return nil
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.want(NewTeamConfig(tc.spec)); err != nil {
				t.Error(err)
			}
		})
	}
}
