package core

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadProjectConfigDogfood(t *testing.T) {
	t.Parallel()

	// The project manages its own backlog with this format, so its own
	// project.yaml is the first configuration that has to load.
	data, err := os.ReadFile(filepath.Join("..", "..", "docs", ".pmngr", "project.yaml"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	cfg, err := LoadProjectConfig(data)
	if err != nil {
		t.Fatalf("LoadProjectConfig(): %v", err)
	}
	if cfg.Key != "GIT" || cfg.Schema != 1 || cfg.Name != "git-in-track" {
		t.Errorf("identity = (%q, %d, %q)", cfg.Key, cfg.Schema, cfg.Name)
	}
	if got := cfg.InitialStatus(); got != "backlog" {
		t.Errorf("InitialStatus() = %q, want backlog", got)
	}
	if got := cfg.CategoryOf("in_review"); got != CategoryInProgress {
		t.Errorf("CategoryOf(in_review) = %q", got)
	}
	if def, ok := cfg.StatusDef("done"); !ok || !def.Terminal {
		t.Errorf("done status = %#v, ok = %t", def, ok)
	}
	if def, _ := cfg.StatusDef("in_progress"); def.WIP != 4 {
		t.Errorf("in_progress wip = %d, want 4", def.WIP)
	}
	if !cfg.HasLabel("Core") {
		t.Error("HasLabel must compare case-insensitively")
	}
	if cfg.IDAllocation.Counters[TypeStory] != 30 {
		t.Errorf("story counter = %d, want 30", cfg.IDAllocation.Counters[TypeStory])
	}
	if len(cfg.Workflow.Transitions[Status("todo")]) != 3 {
		t.Errorf("transitions = %#v", cfg.Workflow.Transitions)
	}
	if cfg.Estimation.TrackHours {
		t.Error("track_hours = true, want false")
	}
	if !cfg.Docs.Wikilinks || cfg.Docs.Math {
		t.Errorf("docs = %#v", cfg.Docs)
	}
	if len(cfg.Validate()) != 0 {
		t.Errorf("diagnostics = %v, want none", cfg.Validate())
	}
}

func TestLoadProjectConfigFixture(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "fixtures", "project-basic", "docs", ".pmngr", "project.yaml"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	cfg, err := LoadProjectConfig(data)
	if err != nil {
		t.Fatalf("LoadProjectConfig(): %v", err)
	}
	if cfg.Key != "DEMO" {
		t.Errorf("key = %q, want DEMO", cfg.Key)
	}
	if !cfg.Estimation.TrackHours {
		t.Error("track_hours = false, want true")
	}
	if got := cfg.Defaults[TypeStory].Status; got != "backlog" {
		t.Errorf("story default status = %q", got)
	}
	if len(cfg.CustomFields) != 2 || cfg.CustomFields[0].Key != "risk" {
		t.Errorf("custom fields = %#v", cfg.CustomFields)
	}
	if len(cfg.People) != 2 || cfg.People[0].Handle != "jose" {
		t.Errorf("people = %#v", cfg.People)
	}
	if cfg.Links == nil || cfg.Links.Host != "github" {
		t.Errorf("links = %#v", cfg.Links)
	}
}

func TestProjectConfigDefaults(t *testing.T) {
	t.Parallel()

	cfg, err := LoadProjectConfig([]byte("schema: 1\nkey: ACME\nname: ACME\nworkflow:\n  statuses:\n    - { id: todo, name: To Do, category: todo }\n    - { id: done, name: Done, category: done, terminal: true }\n"))
	if err != nil {
		t.Fatalf("LoadProjectConfig(): %v", err)
	}
	if cfg.Timezone != "UTC" {
		t.Errorf("timezone = %q, want the UTC default", cfg.Timezone)
	}
	if !cfg.Docs.Wikilinks || !cfg.Docs.Mermaid || !cfg.Docs.Footnotes || !cfg.Docs.Callouts || cfg.Docs.Math {
		t.Errorf("docs defaults = %#v", cfg.Docs)
	}
	if cfg.Docs.AttachmentsDir != ".pmngr/attachments" {
		t.Errorf("attachments_dir = %q", cfg.Docs.AttachmentsDir)
	}
	if cfg.IDAllocation.Strategy != "scan" || !cfg.IDAllocation.WriteCounters {
		t.Errorf("id_allocation defaults = %#v", cfg.IDAllocation)
	}
	if len(cfg.Priorities) != 4 {
		t.Errorf("priorities = %v", cfg.Priorities)
	}
	if got := cfg.InitialStatus(); got != "todo" {
		t.Errorf("InitialStatus() = %q, want the first declared status", got)
	}
}

func TestProjectConfigValidation(t *testing.T) {
	t.Parallel()

	const valid = `schema: 1
key: ACME
name: ACME Platform
workflow:
  initial: todo
  statuses:
    - { id: todo, name: To Do, category: todo }
    - { id: done, name: Done, category: done, terminal: true }
  transitions:
    todo: [done]
`

	tests := []struct {
		name string
		in   string
		want Code
	}{
		{name: "valid", in: valid},
		{name: "missing schema", in: "key: ACME\nname: A\nworkflow:\n  statuses: [{ id: todo, name: To Do, category: todo }]\n", want: CodeProjSchema},
		{name: "future schema", in: "schema: 99\nkey: ACME\nname: A\nworkflow:\n  statuses: [{ id: todo, name: To Do, category: todo }]\n", want: CodeProjSchema},
		{name: "missing key", in: "schema: 1\nname: A\nworkflow:\n  statuses: [{ id: todo, name: To Do, category: todo }]\n", want: CodeProjKey},
		{name: "lowercase key", in: "schema: 1\nkey: acme\nname: A\nworkflow:\n  statuses: [{ id: todo, name: To Do, category: todo }]\n", want: CodeProjKey},
		{name: "no statuses", in: "schema: 1\nkey: ACME\nname: A\n", want: CodeProjInitial},
		{
			name: "duplicate status",
			in:   "schema: 1\nkey: ACME\nname: A\nworkflow:\n  statuses:\n    - { id: todo, name: To Do, category: todo }\n    - { id: todo, name: Again, category: done }\n",
			want: CodeProjStatusDup,
		},
		{
			name: "unknown category",
			in:   "schema: 1\nkey: ACME\nname: A\nworkflow:\n  statuses:\n    - { id: todo, name: To Do, category: pending }\n",
			want: CodeProjStatusCategory,
		},
		{
			name: "unknown initial status",
			in:   "schema: 1\nkey: ACME\nname: A\nworkflow:\n  initial: nowhere\n  statuses:\n    - { id: todo, name: To Do, category: todo }\n",
			want: CodeProjInitial,
		},
		{
			name: "unknown transition target",
			in:   "schema: 1\nkey: ACME\nname: A\nworkflow:\n  statuses:\n    - { id: todo, name: To Do, category: todo }\n  transitions:\n    todo: [nowhere]\n",
			want: CodeProjTransitionTarget,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg, err := LoadProjectConfig([]byte(tt.in))
			if tt.want == "" {
				if err != nil {
					t.Fatalf("LoadProjectConfig(): %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("LoadProjectConfig() = %#v, want %s", cfg, tt.want)
			}
			if cfg == nil {
				t.Fatal("the parsed configuration must be returned even when it is invalid")
			}
			var de *DiagnosticError
			if !errors.As(err, &de) {
				t.Fatalf("err is not a *DiagnosticError: %v", err)
			}
			if !hasDiagnostic(cfg.Validate(), tt.want) {
				t.Errorf("diagnostics = %v, want %s", cfg.Validate(), tt.want)
			}
		})
	}
}

func TestProjectConfigWarnings(t *testing.T) {
	t.Parallel()

	const in = `schema: 1
key: ACME
name: ACME Platform
workflow:
  statuses:
    - { id: todo, name: To Do, category: todo }
    - { id: gone, name: Gone, category: cancelled }
labels:
  - { name: backend }
  - { name: Backend }
`
	cfg, err := LoadProjectConfig([]byte(in))
	if err != nil {
		t.Fatalf("LoadProjectConfig(): %v", err)
	}
	diags := cfg.Validate()
	if !hasDiagnostic(diags, CodeWarnNoDone) {
		t.Errorf("diagnostics = %v, want %s", diags, CodeWarnNoDone)
	}
	if !hasDiagnostic(diags, CodeWarnLabelDup) {
		t.Errorf("diagnostics = %v, want %s", diags, CodeWarnLabelDup)
	}
	for _, d := range diags {
		if d.Severity != SeverityWarning {
			t.Errorf("diagnostic %v is an error, want warnings only", d)
		}
	}
}

func TestProjectConfigInvalidYAML(t *testing.T) {
	t.Parallel()

	if _, err := LoadProjectConfig([]byte("schema: 1\nkey: [unbalanced\n")); err == nil {
		t.Error("LoadProjectConfig() on malformed YAML succeeded, want an error")
	}
}

func TestIDRangeDecoding(t *testing.T) {
	t.Parallel()

	cfg, err := LoadProjectConfig([]byte("schema: 1\nkey: ACME\nname: A\nworkflow:\n  statuses: [{ id: todo, name: To Do, category: todo }]\nid_allocation:\n  reserved:\n    task: [[200, 249]]\n"))
	if err != nil {
		t.Fatalf("LoadProjectConfig(): %v", err)
	}
	ranges := cfg.IDAllocation.Reserved[TypeTask]
	if len(ranges) != 1 || ranges[0].From != 200 || ranges[0].To != 249 {
		t.Errorf("reserved = %#v, want [{200 249}]", ranges)
	}
}

func hasDiagnostic(diags []Diagnostic, code Code) bool {
	for _, d := range diags {
		if d.Code == code {
			return true
		}
	}
	return false
}
