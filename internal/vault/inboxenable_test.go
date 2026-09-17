package vault

import (
	"strings"
	"testing"

	"github.com/digiogithub/git-in-track/internal/core"
)

// legacyConfig is a project.yaml from before the inbox, commented and in the
// flow style this repository's own backlog uses.
const legacyConfig = `# The ACME backlog.
schema: 1
key: ACME
name: ACME Platform
workflow:
  initial: backlog
  statuses:
    # Ordinary work only.
    - {id: backlog, name: Backlog, category: todo}
    - {id: done, name: Done, category: done, terminal: true}
  transitions:
    backlog: [done]
    done: [backlog]
`

const legacyConfigPath = "docs/.pmngr/project.yaml"

// legacyVault returns a vault holding one project whose workflow is config.
func legacyVault(t *testing.T, config string) (*Vault, *core.MemFS) {
	t.Helper()
	fsys := core.NewMemFSFromMap(map[string]string{legacyConfigPath: config})
	v, err := Open(fsys, "repo")
	if err != nil {
		t.Fatalf("open vault: %v", err)
	}
	return v, fsys
}

func TestProjectInboxEnable(t *testing.T) {
	t.Run("adds the triage status first and keeps the rest of the file", func(t *testing.T) {
		v, fsys := legacyVault(t, legacyConfig)
		submission := map[string]any{
			"project": "ACME", "type": "story", "title": "Checkout hangs",
			"inbox": map[string]any{"source": "web"},
		}
		env := rawCall(t, v, "item.create", submission)
		if env.OK || env.Error.Code != NoTriageStatusCode {
			t.Fatalf("filing before = ok:%v code:%q, want %s", env.OK, env.Error.Code, NoTriageStatusCode)
		}
		before := decode[[]projectSummary](t, call(t, v, "project.list", nil))
		if len(before) != 1 || before[0].ConfigRev != string(core.ComputeRev([]byte(legacyConfig))) {
			t.Fatalf("project.list configRev = %+v", before)
		}

		result := decode[ProjectInboxEnabled](t, call(t, v, "project.inbox.enable", map[string]any{
			"project": "ACME", "rev": before[0].ConfigRev,
		}))

		want := strings.Replace(legacyConfig, "    - {id: backlog,",
			"    - {id: triage, name: Triage, category: triage}\n    - {id: backlog,", 1)
		data, err := fsys.ReadFile(legacyConfigPath)
		if err != nil {
			t.Fatalf("read back: %v", err)
		}
		if string(data) != want {
			t.Errorf("project.yaml =\n%s\nwant\n%s", data, want)
		}
		if len(result.Writes.Written) != 1 || result.Writes.Written[0].Path != legacyConfigPath {
			t.Errorf("write set = %+v", result.Writes)
		}
		statuses := result.Project.Statuses
		if len(statuses) != 3 || statuses[0].ID != "triage" || statuses[0].Category != "triage" {
			t.Errorf("statuses = %+v", statuses)
		}
		if result.Project.Workflow == nil || result.Project.Workflow.Initial != "backlog" {
			t.Errorf("workflow = %+v, want initial backlog", result.Project.Workflow)
		}
		if result.Project.ConfigRev != string(core.ComputeRev(data)) {
			t.Errorf("configRev = %q, want the rev of the new file", result.Project.ConfigRev)
		}
		// The vault follows at once: the project now takes submissions.
		call(t, v, "item.create", submission)
	})

	t.Run("refusals carry the stable codes and leave the file alone", func(t *testing.T) {
		enabled := strings.Replace(legacyConfig, "    - {id: backlog,",
			"    - {id: inbox, name: Inbox, category: triage}\n    - {id: backlog,", 1)
		clash := "schema: 1\nkey: ACME\nworkflow:\n  initial: triage\n  statuses:\n" +
			"    - {id: triage, name: Needs a look, category: todo}\n    - {id: done, category: done}\n"
		tests := []struct {
			name   string
			config string
			params map[string]any
			want   string
		}{
			{
				name:   "already enabled",
				config: enabled,
				params: map[string]any{"project": "ACME"},
				want:   InboxEnabledCode,
			},
			{
				name:   "an ordinary status is called triage",
				config: clash,
				params: map[string]any{"project": "ACME"},
				want:   TriageIDTakenCode,
			},
			{
				name:   "a stale revision",
				config: legacyConfig,
				params: map[string]any{"project": "ACME", "rev": "sha256:0000000000000000"},
				want:   core.StaleRevisionCode,
			},
			{
				name:   "an unknown project",
				config: legacyConfig,
				params: map[string]any{"project": "NOPE"},
				want:   "not_found",
			},
		}
		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				v, fsys := legacyVault(t, tc.config)
				env := rawCall(t, v, "project.inbox.enable", tc.params)
				if env.OK {
					t.Fatalf("project.inbox.enable accepted %+v", tc.params)
				}
				if env.Error.Code != tc.want {
					t.Errorf("code = %q, want %q (%s)", env.Error.Code, tc.want, env.Error.Message)
				}
				data, _ := fsys.ReadFile(legacyConfigPath)
				if string(data) != tc.config {
					t.Errorf("a refused call rewrote project.yaml:\n%s", data)
				}
			})
		}
	})

	t.Run("a read-only mount is refused", func(t *testing.T) {
		v, fsys := legacyVault(t, legacyConfig)
		fsys.ReadOnly = true
		env := rawCall(t, v, "project.inbox.enable", map[string]any{"project": "ACME"})
		if env.OK || env.Error.Code != "read_only" {
			t.Fatalf("read-only enable = ok:%v code:%q", env.OK, env.Error.Code)
		}
	})
}
