package vault

import (
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/digiogithub/git-in-track/internal/core"
)

// specLintWire is the result of spec.lint.
type specLintWire struct {
	Findings []SpecLiveFinding `json:"findings"`
}

// deltaPreviewWire is the result of spec.delta.preview.
type deltaPreviewWire struct {
	Operations []DeltaPreviewOperation `json:"operations"`
}

// lintLines renders each a finding as "<code> <severity> <line>".
func lintLines(findings []SpecLiveFinding) []string {
	out := make([]string, 0, len(findings))
	for _, f := range findings {
		out = append(out, string(f.Code)+" "+string(f.Severity)+" "+strconv.Itoa(f.Line))
	}
	return out
}

func TestSpecLint(t *testing.T) {
	const vagueSpec = "## Requirements\n\n" +
		"### DEMO-SP-0001.R1 — Trim input\n\nThe checkout SHALL trim addresses fast.\n\n" +
		"#### Scenario: spaces\n- **WHEN** an address ends in spaces\n- **THEN** they are removed\n\n" +
		"### DEMO-SP-0001.R2 — Reject empty input\n\nthe checkout refuses an empty address.\n"
	const storyDelta = "## Spec Delta\n\n" +
		"### MODIFIED DEMO-SP-0001.R1 — Trim\n\nThe checkout SHALL trim input fast.\n\n" +
		"#### Scenario: spaces\n- **WHEN** it ends in spaces\n- **THEN** they go\n\n" +
		"### MODIFIED DEMO-SP-0001.R9 — Missing\n\nThe checkout SHALL exist.\n\n" +
		"#### Scenario: any\n- **WHEN** asked\n- **THEN** it answers\n"

	tests := []struct {
		name   string
		config string
		params map[string]any
		want   []string
	}{
		{
			name:   "spec body at the default severity",
			params: map[string]any{"project": "DEMO", "id": "DEMO-SP-0001", "type": "spec", "body": vagueSpec},
			want: []string{
				"LINT-REQ-VAGUE warning 5",
				"LINT-REQ-STATEMENT warning 13",
				"LINT-REQ-SCENARIO warning 11",
			},
		},
		{
			name:   "specs.lint error with the scenario rule off",
			config: "specs:\n  lint:\n    severity: error\n    rules:\n      LINT-REQ-SCENARIO: \"off\"\n",
			params: map[string]any{"id": "DEMO-SP-0001", "type": "spec", "body": vagueSpec},
			want:   []string{"LINT-REQ-VAGUE error 5", "LINT-REQ-STATEMENT error 13"},
		},
		{
			name:   "everything off",
			config: "specs:\n  lint: \"off\"\n",
			params: map[string]any{"project": "DEMO", "id": "DEMO-SP-0001", "type": "spec", "body": vagueSpec},
			want:   []string{},
		},
		{
			name:   "story delta: lint and a dangling target",
			params: map[string]any{"project": "DEMO", "type": "story", "body": storyDelta},
			want:   []string{"LINT-REQ-VAGUE warning 5", "W-DELTA-DANGLING warning 11"},
		},
		{
			name:   "an epic has nothing to lint",
			params: map[string]any{"project": "DEMO", "type": "epic", "body": storyDelta},
			want:   []string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := deltaFixtureVault(t, func(s string) string {
				return strings.Replace(s, "\nschema: 1\n", "\nschema: 2\n"+tt.config, 1)
			}, "## Description\n\nNone.\n")
			got := decode[specLintWire](t, call(t, v, "spec.lint", tt.params))
			lines := lintLines(got.Findings)
			want := slices.Clone(tt.want)
			slices.Sort(lines)
			slices.Sort(want)
			if !slices.Equal(lines, want) {
				t.Errorf("findings = %q, want %q", lines, tt.want)
			}
			for _, f := range got.Findings {
				if f.Message == "" {
					t.Errorf("%s has no message", f.Code)
				}
			}
		})
	}

	t.Run("type is required", func(t *testing.T) {
		v := deltaFixtureVault(t, func(s string) string { return s }, "None.\n")
		env := rawCall(t, v, "spec.lint", map[string]any{"body": "x"})
		if env.OK || env.Error.Code != "invalid_request" {
			t.Errorf("ok %v code %q, want invalid_request", env.OK, env.Error.Code)
		}
	})
}

func TestSpecDeltaPreview(t *testing.T) {
	v := deltaFixtureVault(t, func(s string) string { return s }, deltaBody)
	body := deltaBody + "\n## Spec Delta\n\n### MODIFIED DEMO-SP-0001.R7 — Nowhere\n\nThe checkout SHALL vanish.\n\n" +
		"### ADDED DEMO-SP-0001 — Move trimming\n\nSupersedes: DEMO-SP-0001.R1\n\nThe checkout SHALL trim on blur.\n"
	got := decode[deltaPreviewWire](t, call(t, v, "spec.delta.preview", map[string]any{
		"project": "DEMO", "id": "DEMO-US-0003", "body": body,
	}))
	if len(got.Operations) != 5 {
		t.Fatalf("operations = %d, want 5: %+v", len(got.Operations), got.Operations)
	}
	added, modified, removed, dangling, moved := got.Operations[0], got.Operations[1], got.Operations[2], got.Operations[3], got.Operations[4]

	if added.Op != "ADDED" || added.Target != "DEMO-SP-0001" || added.SpecTitle != "Checkout addresses" ||
		added.Current != nil || !strings.HasPrefix(added.Proposed, "The checkout SHALL refuse an address") ||
		added.Dangling != "" {
		t.Errorf("added = %+v", added)
	}
	if modified.Op != "MODIFIED" || modified.Target != "DEMO-SP-0001.R1" || modified.Current == nil ||
		!strings.HasPrefix(modified.Current.Text, "The checkout SHALL trim pasted addresses.") ||
		modified.Current.Title != "Trim input" ||
		modified.Proposed != "The checkout SHALL trim pasted addresses on both ends." {
		t.Errorf("modified = %+v current %+v", modified, modified.Current)
	}
	if removed.Op != "REMOVED" || removed.Current == nil || removed.Current.Ref != "DEMO-SP-0001.R2" ||
		removed.Reason == "" || removed.Proposed != "" {
		t.Errorf("removed = %+v", removed)
	}
	if dangling.Current != nil || !strings.Contains(dangling.Dangling, "unknown requirement DEMO-SP-0001.R7") {
		t.Errorf("dangling = %+v", dangling)
	}
	if moved.Supersedes != "DEMO-SP-0001.R1" || moved.Current == nil || moved.Current.Ref != "DEMO-SP-0001.R1" ||
		moved.Proposed != "The checkout SHALL trim on blur." {
		t.Errorf("moved = %+v current %+v", moved, moved.Current)
	}

	// A body without a Spec Delta previews nothing, and a workspace routes the
	// call by project like any other.
	w := NewWorkspace()
	if _, err := w.Attach("repo", RoleProject, v); err != nil {
		t.Fatal(err)
	}
	out, err := w.Dispatch(t.Context(), "spec.delta.preview", []byte(`{"project":"DEMO","body":"## Notes\n"}`))
	if err != nil {
		t.Fatal(err)
	}
	if ops := out.(map[string]any)["operations"].([]DeltaPreviewOperation); len(ops) != 0 {
		t.Errorf("operations = %+v, want none", ops)
	}
}

// TestSpecLintLinesAreBodyLines pins the one place the two line numberings
// meet (GIT-US-0144): spec.lint answers on lines of the body it was sent, which
// is the text the web editor holds, while item.validate — like doctor and the
// CLI — answers on lines of the file, below the front matter.
func TestSpecLintLinesAreBodyLines(t *testing.T) {
	const body = "## Spec Delta\n\n" +
		"### MODIFIED DEMO-SP-0001.R1 — Trim\n\nThe checkout SHALL trim input fast.\n\n" +
		"#### Scenario: spaces\n- **WHEN** it ends in spaces\n- **THEN** they go\n"
	v := deltaFixtureVault(t, func(s string) string {
		return strings.Replace(s, "\nschema: 1\n", "\nschema: 2\n", 1)
	}, body)

	lint := decode[specLintWire](t, call(t, v, "spec.lint", map[string]any{"project": "DEMO", "type": "story", "body": body}))
	diags := decode[[]core.Diagnostic](t, call(t, v, "item.validate", map[string]any{"id": "DEMO-US-0003"}))

	// deltaFixtureVault writes the story as eight lines of front matter and
	// fences, a blank line, then the body.
	const offset = 9
	var bodyLine, fileLine int
	for _, f := range lint.Findings {
		if f.Code == core.LintReqVague {
			bodyLine = f.Line
		}
	}
	for _, d := range diags {
		if d.Code == core.LintReqVague {
			fileLine = d.Line
		}
	}
	if bodyLine != 5 {
		t.Errorf("spec.lint: LINT-REQ-VAGUE on line %d of the body, want 5", bodyLine)
	}
	if fileLine != bodyLine+offset {
		t.Errorf("item.validate: LINT-REQ-VAGUE on line %d of the file, want %d", fileLine, bodyLine+offset)
	}
}
