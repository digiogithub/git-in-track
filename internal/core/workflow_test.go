package core

import "testing"

// triageWorkflowYAML declares an inbox and a transition graph that says nothing
// about the triage status, as a hand-edited project.yaml usually does.
const triageWorkflowYAML = `schema: 1
key: TRI
name: Triage Fixture
workflow:
  initial: backlog
  statuses:
    - { id: triage,      name: Triage,      category: triage }
    - { id: backlog,     name: Backlog,     category: todo }
    - { id: todo,        name: To Do,       category: todo }
    - { id: in_progress, name: In Progress, category: in_progress }
    - { id: done,        name: Done,        category: done, terminal: true }
    - { id: cancelled,   name: Cancelled,   category: cancelled, terminal: true }
  transitions:
    backlog:     [todo, cancelled]
    todo:        [in_progress, backlog, cancelled]
    in_progress: [done, todo]
`

func triageTransitionConfig(t *testing.T, explicit []Status) *ProjectConfig {
	t.Helper()
	cfg, err := LoadProjectConfig([]byte(triageWorkflowYAML))
	if err != nil {
		t.Fatalf("LoadProjectConfig(): %v", err)
	}
	if explicit != nil {
		cfg.Workflow.Transitions["triage"] = explicit
	}
	return cfg
}

// TestValidateTransitionOutOfTriage pins R-INBOX-7: work leaves triage by being
// accepted or rejected, so a transitions mapping without a triage key does not
// trap the inbox. An explicit triage entry is still respected, and moving into
// triage follows the ordinary rules.
func TestValidateTransitionOutOfTriage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		explicit []Status
		from, to Status
		wantCode Code
	}{
		{name: "accept into the initial status", from: "triage", to: "backlog"},
		{name: "accept into a chosen status", from: "triage", to: "in_progress"},
		{name: "reject into cancelled", from: "triage", to: "cancelled"},
		{name: "leaving triage still needs a declared target", from: "triage", to: "shipped", wantCode: CodeStatusUnknown},
		{name: "an explicit triage entry allows what it lists", explicit: []Status{"backlog"}, from: "triage", to: "backlog"},
		{name: "an explicit triage entry flags what it omits", explicit: []Status{"backlog"}, from: "triage", to: "todo", wantCode: CodeWarnWorkflowTransition},
		{name: "moving into triage follows the ordinary rules", from: "backlog", to: "triage", wantCode: CodeWarnWorkflowTransition},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := triageTransitionConfig(t, tt.explicit)
			if got := ValidateTransition(cfg, tt.from, tt.to); got.Code != tt.wantCode {
				t.Fatalf("ValidateTransition(%q, %q) = %q (%s), want %q", tt.from, tt.to, got.Code, got.Message, tt.wantCode)
			}
		})
	}
}
