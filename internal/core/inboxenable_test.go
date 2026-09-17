package core

import (
	"errors"
	"strings"
	"testing"
)

// legacyFlowConfig is a project.yaml written before the inbox existed, in the
// flow style this repository's own backlog uses.
const legacyFlowConfig = `# docs/.pmngr/project.yaml
# A backlog from before the inbox.
schema: 1
key: ACME
name: ACME Platform
description: >-
  A long folded description that must survive the edit unchanged.
workflow:
  initial: backlog
  statuses:
    # Ordinary work.
    - {id: backlog, name: Backlog, category: todo}
    - {id: in_progress, name: In Progress, category: in_progress, wip: 4}
    - {id: done, name: Done, category: done, terminal: true}
  transitions:
    backlog: [in_progress]
    in_progress: [done, backlog]
labels:
  - {name: core, color: "#4f46e5"} # trailing comment
`

func TestEnableInbox(t *testing.T) {
	t.Run("flow entries get a flow line and nothing else moves", func(t *testing.T) {
		out, err := EnableInbox([]byte(legacyFlowConfig))
		if err != nil {
			t.Fatalf("EnableInbox: %v", err)
		}
		want := strings.Replace(legacyFlowConfig,
			"    - {id: backlog,",
			"    - {id: triage, name: Triage, category: triage}\n    - {id: backlog,", 1)
		if string(out) != want {
			t.Errorf("EnableInbox wrote\n%s\nwant\n%s", out, want)
		}
		assertInboxWorkflow(t, out, 4)
	})

	t.Run("block entries get a block entry aligned with them", func(t *testing.T) {
		in := "schema: 1\nkey: ACME\nworkflow:\n  initial: todo\n  statuses:\n" +
			"  - id: todo\n    category: todo\n  - id: done\n    category: done\n"
		out, err := EnableInbox([]byte(in))
		if err != nil {
			t.Fatalf("EnableInbox: %v", err)
		}
		want := "schema: 1\nkey: ACME\nworkflow:\n  initial: todo\n  statuses:\n" +
			"  - id: triage\n    name: Triage\n    category: triage\n" +
			"  - id: todo\n    category: todo\n  - id: done\n    category: done\n"
		if string(out) != want {
			t.Errorf("EnableInbox wrote\n%s\nwant\n%s", out, want)
		}
		assertInboxWorkflow(t, out, 3)
	})

	t.Run("a flow sequence falls back to the node tree", func(t *testing.T) {
		in := "# keep me\nschema: 1\nkey: ACME\nworkflow:\n  initial: todo\n" +
			"  statuses: [{id: todo, category: todo}, {id: done, category: done}]\n"
		out, err := EnableInbox([]byte(in))
		if err != nil {
			t.Fatalf("EnableInbox: %v", err)
		}
		if !strings.HasPrefix(string(out), "# keep me\n") {
			t.Errorf("the leading comment was lost:\n%s", out)
		}
		assertInboxWorkflow(t, out, 3)
	})

	t.Run("a workflow without statuses gets a list", func(t *testing.T) {
		out, err := EnableInbox([]byte("schema: 1\nkey: ACME\n"))
		if err != nil {
			t.Fatalf("EnableInbox: %v", err)
		}
		assertInboxWorkflow(t, out, 1)
	})

	t.Run("refusals", func(t *testing.T) {
		tests := []struct {
			name string
			in   string
			want error
		}{
			{
				name: "already enabled",
				in:   "schema: 1\nkey: ACME\nworkflow:\n  statuses:\n    - {id: inbox, category: triage}\n",
				want: ErrInboxEnabled,
			},
			{
				name: "id taken by an ordinary status",
				in:   "schema: 1\nkey: ACME\nworkflow:\n  statuses:\n    - {id: triage, category: todo}\n",
				want: ErrTriageIDTaken,
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				if _, err := EnableInbox([]byte(tt.in)); !errors.Is(err, tt.want) {
					t.Errorf("err = %v, want %v", err, tt.want)
				}
			})
		}
	})
}

// assertInboxWorkflow checks that data holds a workflow of n statuses whose
// first is the inbox, with initial untouched by the edit.
func assertInboxWorkflow(t *testing.T, data []byte, n int) {
	t.Helper()
	cfg, err := LoadProjectConfig(data)
	if cfg == nil {
		t.Fatalf("the result does not parse: %v\n%s", err, data)
	}
	statuses := cfg.Workflow.Statuses
	if len(statuses) != n || statuses[0] != TriageStatusDef() {
		t.Errorf("statuses = %+v, want %d starting with triage", statuses, n)
	}
	if cfg.Workflow.TriageStatus() != "triage" {
		t.Errorf("TriageStatus() = %q", cfg.Workflow.TriageStatus())
	}
}
