package vault

import (
	"testing"

	"github.com/digiogithub/git-in-track/internal/core"
)

func TestTeamCreate(t *testing.T) {
	t.Run("turns a folder into a team repository", func(t *testing.T) {
		v := emptyVault(t, map[string]string{"README.md": "# team\n"})
		if v.Team() != nil {
			t.Fatal("a fresh repository already reports a team")
		}

		result := decode[teamCreated](t, call(t, v, "team.create", map[string]any{
			"key":  "ACME-TEAM",
			"name": "ACME Delivery Team",
		}))

		wrote := false
		for _, f := range result.Writes.Written {
			if f.Path == core.TeamFileName {
				wrote = true
			}
		}
		if !wrote {
			t.Errorf("the write set does not carry %s: %+v", core.TeamFileName, result.Writes.Written)
		}
		team := v.Team()
		if team == nil || team.Key != "ACME-TEAM" {
			t.Fatalf("the vault does not report the team it just created: %+v", team)
		}
		for _, d := range team.Diagnostics {
			if d.Severity == core.SeverityError {
				t.Errorf("a freshly created team reports an error: %+v", d)
			}
		}
		// The team surface answers immediately, through the normal contract.
		call(t, v, "team.get", nil)
	})

	t.Run("a member is carried into the file", func(t *testing.T) {
		v := emptyVault(t, map[string]string{"README.md": "# team\n"})
		call(t, v, "team.create", map[string]any{
			"key": "ACME",
			"members": []map[string]any{
				{"handle": "jose", "name": "Jose Ruiz", "emails": []string{"jose@example.com"}, "active": true},
			},
		})
		team := v.Team()
		if team == nil || team.Config == nil || len(team.Config.Members) != 1 {
			t.Fatalf("members = %+v", team)
		}
		if got := team.Config.Members[0].Handle; got != "jose" {
			t.Errorf("handle = %q, want jose", got)
		}
	})

	t.Run("refusals carry the stable codes", func(t *testing.T) {
		tests := []struct {
			name   string
			seed   map[string]string
			params map[string]any
			want   string
		}{
			{
				name:   "a lowercase key",
				seed:   map[string]string{"README.md": "# t\n"},
				params: map[string]any{"key": "acme-team"},
				want:   "validation_failed",
			},
			{
				name:   "no key at all",
				seed:   map[string]string{"README.md": "# t\n"},
				params: map[string]any{},
				want:   "validation_failed",
			},
			{
				name:   "a malformed member handle",
				seed:   map[string]string{"README.md": "# t\n"},
				params: map[string]any{"key": "ACME", "members": []map[string]any{{"handle": "Jose"}}},
				want:   "validation_failed",
			},
			{
				name:   "a folder that already holds a team",
				seed:   map[string]string{"team.yaml": "schema: 1\nkey: OLD\nname: Old\n"},
				params: map[string]any{"key": "NEW"},
				want:   "team_exists",
			},
		}
		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				v := emptyVault(t, tc.seed)
				env := rawCall(t, v, "team.create", tc.params)
				if env.OK {
					t.Fatalf("team.create accepted %+v", tc.params)
				}
				if env.Error.Code != tc.want {
					t.Errorf("code = %q, want %q (%s)", env.Error.Code, tc.want, env.Error.Message)
				}
			})
		}
	})
}
