package hub

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	t.Parallel()
	const valid = `
dataDir: data
organization:
  id: acme
  team: {id: team, url: "git@github.com:acme/team.git", deployKey: keys/team}
  projects:
    - {id: web, url: "https://github.com/acme/web.git"}
`
	t.Run("defaults and relative paths", func(t *testing.T) {
		t.Parallel()
		cfg, err := Parse([]byte(valid), "/etc/hub")
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}
		if cfg.DataDir != filepath.Join("/etc/hub", "data") {
			t.Errorf("DataDir = %q", cfg.DataDir)
		}
		if cfg.Organization.Team.DeployKey != filepath.Join("/etc/hub", "keys/team") {
			t.Errorf("DeployKey = %q", cfg.Organization.Team.DeployKey)
		}
		if cfg.Bind != DefaultBind || cfg.Port != DefaultPort {
			t.Errorf("listener = %s:%d", cfg.Bind, cfg.Port)
		}
		if time.Duration(cfg.PullInterval) != DefaultPullInterval {
			t.Errorf("PullInterval = %s", time.Duration(cfg.PullInterval))
		}
		if got := cfg.Repos(); len(got) != 2 || got[0].ID != "team" {
			t.Errorf("Repos = %+v, want the team first", got)
		}
	})

	cases := []struct {
		name, yaml, wantField string
	}{
		{"missing data dir", "organization: {id: acme, projects: [{id: a, url: x}]}", "dataDir: required"},
		{"no project", "dataDir: d\norganization: {id: acme}", "organization.projects: at least one"},
		{"bad org id", "dataDir: d\norganization: {id: Acme, projects: [{id: a, url: x}]}", "organization.id"},
		{"bad repo id", "dataDir: d\norganization: {id: acme, projects: [{id: A_B, url: x}]}", "organization.projects[0].id"},
		{"duplicate repo id", "dataDir: d\norganization: {id: acme, projects: [{id: a, url: x}, {id: a, url: y}]}", "organization.projects[1].id"},
		{"missing url", "dataDir: d\norganization: {id: acme, projects: [{id: a}]}", "organization.projects[0].url: required"},
		{"deploy key on https", "dataDir: d\norganization: {id: acme, projects: [{id: a, url: \"https://h/r.git\", deployKey: k}]}", "organization.projects[0].deployKey"},
		{"interval too short", "dataDir: d\npullInterval: 1s\norganization: {id: acme, projects: [{id: a, url: x}]}", "pullInterval"},
		{"bad duration", "dataDir: d\npullInterval: soon\norganization: {id: acme, projects: [{id: a, url: x}]}", "duration"},
		{"unknown key", "dataDir: d\nbogus: 1\norganization: {id: acme, projects: [{id: a, url: x}]}", "bogus"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := Parse([]byte(tc.yaml), "")
			if err == nil || !strings.Contains(err.Error(), tc.wantField) {
				t.Fatalf("Parse error = %v, want it to mention %q", err, tc.wantField)
			}
		})
	}
}
