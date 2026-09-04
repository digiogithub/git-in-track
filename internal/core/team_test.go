package core

import (
	"os"
	"strings"
	"testing"
)

// teamFixture is the team repository the tests of this file are built from.
const teamFixture = "../../testdata/fixtures/team-basic"

// teamYAML is a minimal, valid team.yaml the negative cases are derived from.
const teamYAML = `
schema: 1
key: ACME-TEAM
name: ACME Delivery Team
members:
  - handle: jose
    name: Jose Ruiz
    emails: [jose@example.com]
projects:
  - key: ACME
    name: ACME Platform
    repo: https://github.com/acme/platform.git
    docs_path: docs
`

func TestLoadTeamConfig(t *testing.T) {
	cfg, err := LoadTeamConfig([]byte(teamYAML))
	if err != nil {
		t.Fatalf("LoadTeamConfig: %v", err)
	}
	if cfg.Key != "ACME-TEAM" {
		t.Errorf("key = %q, want ACME-TEAM", cfg.Key)
	}
	if got := cfg.KnowledgePath(); got != DefaultKnowledgePath {
		t.Errorf("knowledge path = %q, want %q", got, DefaultKnowledgePath)
	}
	if cfg.Timezone != "UTC" {
		t.Errorf("timezone = %q, want the UTC default", cfg.Timezone)
	}
	member, ok := cfg.Member("jose")
	if !ok {
		t.Fatal("member jose is missing")
	}
	if !member.Active {
		t.Error("a member that does not say otherwise must be active (R-MEM-3)")
	}
	if _, ok := cfg.MemberByEmail("JOSE@example.com"); !ok {
		t.Error("MemberByEmail must match a git identity case-insensitively (R-MEM-2)")
	}
	if _, ok := cfg.Project("ACME"); !ok {
		t.Error("project ACME is missing")
	}
}

func TestTeamConfigValidate(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want Code
	}{
		{
			name: "missing schema",
			yaml: strings.Replace(teamYAML, "schema: 1", "", 1),
			want: CodeTeamSchema,
		},
		{
			name: "future schema",
			yaml: strings.Replace(teamYAML, "schema: 1", "schema: 99", 1),
			want: CodeTeamSchema,
		},
		{
			name: "malformed key",
			yaml: strings.Replace(teamYAML, "key: ACME-TEAM", "key: acme team", 1),
			want: CodeTeamKey,
		},
		{
			name: "duplicate project key",
			yaml: teamYAML + `  - key: ACME
    name: Twin
    repo: https://github.com/acme/twin.git
    docs_path: docs
`,
			want: CodeTeamKeyDup,
		},
		{
			name: "duplicate member handle",
			yaml: strings.Replace(teamYAML, "projects:", `  - handle: jose
    name: Other Jose
    emails: [other@example.com]
projects:`, 1),
			want: CodeTeamHandleDup,
		},
		{
			name: "email claimed twice",
			yaml: strings.Replace(teamYAML, "projects:", `  - handle: marta
    name: Marta Alonso
    emails: [jose@example.com]
projects:`, 1),
			want: CodeTeamEmailDup,
		},
		{
			name: "project without repo",
			yaml: strings.Replace(teamYAML, "    repo: https://github.com/acme/platform.git\n", "", 1),
			want: CodeTeamProjectFields,
		},
		{
			name: "no web url and no https remote",
			yaml: strings.Replace(teamYAML,
				"repo: https://github.com/acme/platform.git",
				"repo: git@github.com:acme/platform.git", 1),
			want: CodeTeamWebURL,
		},
		{
			name: "no member",
			yaml: strings.Replace(teamYAML, `members:
  - handle: jose
    name: Jose Ruiz
    emails: [jose@example.com]
`, "", 1),
			want: CodeTeamMemberFields,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := LoadTeamConfig([]byte(tt.yaml))
			if cfg == nil {
				t.Fatalf("LoadTeamConfig returned no configuration: %v", err)
			}
			if !hasCode(cfg.Validate(), tt.want) {
				t.Fatalf("diagnostics %v do not contain %s", cfg.Validate(), tt.want)
			}
		})
	}
}

// hasCode reports whether a diagnostic list carries a code.
func hasCode(diags []Diagnostic, code Code) bool {
	for _, d := range diags {
		if d.Code == code {
			return true
		}
	}
	return false
}

func TestParseRef(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    Ref
		wantErr bool
	}{
		{name: "story", in: "ACME/ACME-US-0042", want: Ref{Project: "ACME", Item: "ACME-US-0042"}},
		{name: "task", in: "WEB/WEB-T-0107", want: Ref{Project: "WEB", Item: "WEB-T-0107"}},
		{name: "surrounding space", in: "  ACME/ACME-EP-0001 ", want: Ref{Project: "ACME", Item: "ACME-EP-0001"}},
		{name: "no slash", in: "ACME-US-0042", wantErr: true},
		{name: "lowercase key", in: "acme/ACME-US-0042", wantErr: true},
		{name: "malformed item", in: "ACME/ACME-XX-0042", wantErr: true},
		{name: "key disagrees with the item", in: "WEB/ACME-US-0042", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseRef(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseRef(%q) = %v, want an error", tt.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseRef(%q): %v", tt.in, err)
			}
			if got != tt.want {
				t.Errorf("ParseRef(%q) = %+v, want %+v", tt.in, got, tt.want)
			}
			if got.String() != strings.TrimSpace(tt.in) {
				t.Errorf("String() = %q, want %q", got.String(), strings.TrimSpace(tt.in))
			}
		})
	}
}

func TestDiscoverTeam(t *testing.T) {
	t.Run("fixture", func(t *testing.T) {
		team, ok, err := DiscoverTeam(testDirFS{root: teamFixture}, ".")
		if err != nil {
			t.Fatalf("DiscoverTeam: %v", err)
		}
		if !ok {
			t.Fatal("the team-basic fixture must be discovered as a team repository")
		}
		for _, d := range team.Diagnostics {
			if d.Severity == SeverityError {
				t.Errorf("unexpected error diagnostic: %s", d)
			}
		}
		if team.Key != "DEMO-TEAM" {
			t.Errorf("key = %q, want DEMO-TEAM", team.Key)
		}
		if team.KnowledgePath != "knowledge" {
			t.Errorf("knowledge path = %q, want knowledge", team.KnowledgePath)
		}
		if len(team.Config.Projects) != 2 {
			t.Fatalf("projects = %d, want 2", len(team.Config.Projects))
		}
		scope := team.KBScope()
		if !scope.Team || scope.DocsPath != "knowledge" {
			t.Errorf("KBScope = %+v, want the knowledge folder marked as a team scope", scope)
		}
	})

	t.Run("no team.yaml", func(t *testing.T) {
		_, ok, err := DiscoverTeam(NewMemFS(), ".")
		if err != nil {
			t.Fatalf("DiscoverTeam: %v", err)
		}
		if ok {
			t.Error("an empty vault is not a team repository")
		}
	})

	t.Run("backlog in the team repository", func(t *testing.T) {
		data, err := os.ReadFile(teamFixture + "/team.yaml")
		if err != nil {
			t.Fatalf("read fixture: %v", err)
		}
		m := NewMemFSFromMap(map[string]string{
			"team.yaml":                      string(data),
			".pmngr/stories/DEMO-US-0001.md": "---\nid: DEMO-US-0001\n---\n",
		})
		team, ok, err := DiscoverTeam(m, ".")
		if err != nil || !ok {
			t.Fatalf("DiscoverTeam: %v (ok=%v)", err, ok)
		}
		if !hasCode(team.Diagnostics, CodeTeamBacklogInTeamRepo) {
			t.Fatalf("diagnostics %v do not report a backlog in the team repository", team.Diagnostics)
		}
	})
}
