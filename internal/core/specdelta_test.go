package core

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// specDeltaFixture reads the story whose Spec Delta exercises every operation
// and every structural finding.
func specDeltaFixture(t *testing.T) *Item {
	t.Helper()
	src, err := os.ReadFile(filepath.Join("testdata", "spec-delta-story.md"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	it, err := ParseItem("stories/ACME-US-0042-harden-reserved-ranges.md", src)
	if err != nil {
		t.Fatalf("ParseItem(): %v", err)
	}
	return it
}

// deltaConfig is a project with a todo, an in-progress, a done and a
// cancelled status, at the default lint severity.
func deltaConfig() *ProjectConfig {
	return &ProjectConfig{
		Schema: 2, Key: "ACME", Name: "Acme",
		Workflow: Workflow{Statuses: []StatusDef{
			{ID: "todo", Category: CategoryTodo},
			{ID: "in_progress", Category: CategoryInProgress},
			{ID: "done", Category: CategoryDone},
			{ID: "cancelled", Category: CategoryCancelled},
		}},
	}
}

func TestParseSpecDeltaGolden(t *testing.T) {
	t.Parallel()

	it := specDeltaFixture(t)
	delta := ParseSpecDelta(it.Body)
	for _, op := range delta.Operations {
		if it.Body[op.Start:op.End] != op.Text {
			t.Errorf("%s %s: body[%d:%d] is not the operation text", op.Op, op.Target(), op.Start, op.End)
		}
	}
	got, err := json.MarshalIndent(delta, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	compareGolden(t, "spec-delta.json", append(got, '\n'))
}

func TestSpecDeltaDiagnosticsGolden(t *testing.T) {
	t.Parallel()

	it := specDeltaFixture(t)
	var b strings.Builder
	b.WriteString("# SpecDeltaDiagnostics(spec-delta-story.md) at status in_progress, default lint\n")
	for _, d := range SpecDeltaDiagnostics(it, deltaConfig()) {
		fmt.Fprintf(&b, "%s:%d %s %s %s: %s\n", filepath.Base(d.Path), d.Line, d.Field, d.Severity, d.Code, d.Message)
	}
	compareGolden(t, "spec-delta-diagnostics.txt", []byte(b.String()))
}

func TestParseSpecDeltaHeadings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		heading string
		wantOp  DeltaOp
		wantTgt string
		code    Code // the one finding, if any
	}{
		{name: "added", heading: "### ADDED ACME-SP-0003 — New", wantOp: DeltaAdded, wantTgt: "ACME-SP-0003"},
		{name: "modified", heading: "### MODIFIED ACME-SP-0003.R2 — Changed", wantOp: DeltaModified, wantTgt: "ACME-SP-0003.R2"},
		{name: "removed", heading: "### REMOVED ACME-SP-0003.R4 — Gone", wantOp: DeltaRemoved, wantTgt: "ACME-SP-0003.R4"},
		{name: "extra blanks between the parts", heading: "### MODIFIED   ACME-SP-0003.R2 —   Changed", wantOp: DeltaModified, wantTgt: "ACME-SP-0003.R2"},
		{name: "en dash separator", heading: "### MODIFIED ACME-SP-0003.R2 – Changed", wantOp: DeltaModified, wantTgt: "ACME-SP-0003.R2", code: CodeWarnReqSeparator},
		{name: "added with a number", heading: "### ADDED ACME-SP-0003.R9 — Applied", wantOp: DeltaAdded, wantTgt: "ACME-SP-0003.R9"},
		{name: "lower-case operation", heading: "### modified ACME-SP-0003.R2 — Changed", code: CodeDeltaOp},
		{name: "unknown operation", heading: "### RENAMED ACME-SP-0003.R2 — Changed", code: CodeDeltaOp},
		{name: "no target", heading: "### ADDED", code: CodeDeltaOp},
		{name: "no separator", heading: "### MODIFIED ACME-SP-0003.R2 Changed", code: CodeDeltaOp},
		{name: "no title", heading: "### MODIFIED ACME-SP-0003.R2 —", code: CodeDeltaOp},
		{name: "target is prose", heading: "### ADDED the allocator — New", code: CodeDeltaOp},
		{name: "modified names a spec", heading: "### MODIFIED ACME-SP-0003 — Changed", code: CodeDeltaTarget},
		{name: "removed names a spec", heading: "### REMOVED ACME-SP-0003 — Gone", code: CodeDeltaTarget},
		{name: "names a story", heading: "### ADDED ACME-US-0003 — New", code: CodeDeltaTarget},
		{name: "padded number", heading: "### REMOVED ACME-SP-0003.R04 — Gone", code: CodeIDGrammar},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			body := "## Spec Delta\n\n" + tt.heading + "\n\nThe system SHALL act.\n\nReason: because.\n"
			delta := ParseSpecDelta(body)
			var codes []Code
			for _, f := range delta.Findings {
				codes = append(codes, f.Code)
			}
			var wantCodes []Code
			if tt.code != "" {
				wantCodes = []Code{tt.code}
			}
			if !reflect.DeepEqual(codes, wantCodes) {
				t.Errorf("findings = %v, want %v", codes, wantCodes)
			}
			if tt.wantOp == "" {
				if len(delta.Operations) != 0 {
					t.Errorf("operations = %+v, want none", delta.Operations)
				}
				return
			}
			if len(delta.Operations) != 1 {
				t.Fatalf("operations = %+v, want one", delta.Operations)
			}
			op := delta.Operations[0]
			if op.Op != tt.wantOp || op.Target() != tt.wantTgt {
				t.Errorf("operation = %s %s, want %s %s", op.Op, op.Target(), tt.wantOp, tt.wantTgt)
			}
		})
	}
}

func TestParseSpecDeltaContent(t *testing.T) {
	t.Parallel()

	t.Run("no section", func(t *testing.T) {
		t.Parallel()
		got := ParseSpecDelta("## Description\n\n### ADDED ACME-SP-0003 — New\n\nThe system SHALL act.\n")
		if len(got.Operations) != 0 || len(got.Findings) != 0 {
			t.Errorf("ParseSpecDelta() = %+v, want nothing", got)
		}
	})
	t.Run("CRLF and a case-insensitive section heading", func(t *testing.T) {
		t.Parallel()
		got := ParseSpecDelta("## spec delta\r\n\r\n### REMOVED ACME-SP-0003.R4 — Gone\r\n\r\nReason: obsolete.\r\n")
		if len(got.Operations) != 1 || got.Operations[0].Reason != "obsolete." || len(got.Findings) != 0 {
			t.Errorf("ParseSpecDelta() = %+v", got)
		}
	})
	t.Run("empty reason", func(t *testing.T) {
		t.Parallel()
		got := ParseSpecDelta("## Spec Delta\n\n### REMOVED ACME-SP-0003.R4 — Gone\n\nReason:\n")
		if len(got.Findings) != 1 || got.Findings[0].Code != CodeDeltaReason {
			t.Errorf("findings = %+v, want one %s", got.Findings, CodeDeltaReason)
		}
	})
	t.Run("reason inside a fence does not count", func(t *testing.T) {
		t.Parallel()
		got := ParseSpecDelta("## Spec Delta\n\n### REMOVED ACME-SP-0003.R4 — Gone\n\n```\nReason: quoted\n```\n")
		if len(got.Findings) != 1 || got.Findings[0].Code != CodeDeltaReason {
			t.Errorf("findings = %+v, want one %s", got.Findings, CodeDeltaReason)
		}
	})
	t.Run("supersedes is not the statement", func(t *testing.T) {
		t.Parallel()
		got := ParseSpecDelta("## Spec Delta\n\n### ADDED ACME-SP-0005 — Moved\nSupersedes: ACME-SP-0003.R7\n\nThe system SHALL act.\n")
		if len(got.Operations) != 1 {
			t.Fatalf("operations = %+v", got.Operations)
		}
		op := got.Operations[0]
		if op.Supersedes == nil || op.Supersedes.String() != "ACME-SP-0003.R7" || op.Statement != "The system SHALL act." {
			t.Errorf("operation = %+v", op)
		}
		if !reflect.DeepEqual(got.Refs(), []RequirementRef{{Spec: "ACME-SP-0003", Number: 7}}) {
			t.Errorf("Refs() = %v", got.Refs())
		}
	})
	t.Run("a later level-2 heading ends the section", func(t *testing.T) {
		t.Parallel()
		got := ParseSpecDelta("## Spec Delta\n\n### REMOVED ACME-SP-0003.R4 — Gone\n\nReason: x.\n\n## Notes\n\n### REMOVED ACME-SP-0003.R5 — Gone\n")
		if len(got.Operations) != 1 || !strings.HasSuffix(got.Operations[0].Text, "Reason: x.\n\n") {
			t.Errorf("operations = %+v", got.Operations)
		}
	})
}

func TestSpecDeltaDiagnosticsDependOnStatusAndType(t *testing.T) {
	t.Parallel()

	body := "## Spec Delta\n\n### ADDED ACME-SP-0003.R9 — Applied\n\nThe system SHALL act.\n\n" +
		"#### Scenario: acts\n- **WHEN** asked\n- **THEN** it acts\n"
	tests := []struct {
		name   string
		typ    ItemType
		status Status
		cfg    *ProjectConfig
		links  []Link
		want   []Code
	}{
		{name: "numbered ADDED before done", typ: TypeStory, status: "in_progress", cfg: deltaConfig(), want: []Code{CodeDeltaTarget}},
		{
			name: "numbered ADDED on a reopened story that implements it", typ: TypeStory, status: "in_progress", cfg: deltaConfig(),
			links: []Link{{Kind: LinkImplements, Target: "ACME-SP-0003.R9"}},
		},
		{
			name: "a relates_to link does not record the application", typ: TypeStory, status: "in_progress", cfg: deltaConfig(),
			links: []Link{{Kind: LinkRelatesTo, Target: "ACME-SP-0003.R9"}}, want: []Code{CodeDeltaTarget},
		},
		{name: "numbered ADDED on a done task is the applied form", typ: TypeTask, status: "done", cfg: deltaConfig()},
		{name: "no project configuration skips the status rule", typ: TypeStory, status: "todo"},
		{name: "an epic's Spec Delta is prose", typ: TypeEpic, status: "todo", cfg: deltaConfig()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			it := &Item{ID: "ACME-US-0001", Type: tt.typ, Status: tt.status, Body: body, Links: tt.links}
			var got []Code
			for _, d := range SpecDeltaDiagnostics(it, tt.cfg) {
				got = append(got, d.Code)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("codes = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSpecDeltaLintFollowsSpecsLint(t *testing.T) {
	t.Parallel()

	body := "## Spec Delta\n\n### MODIFIED ACME-SP-0003.R2 — Vague\n\nThe system SHALL act fast.\n\n" +
		"#### Scenario: acts\n- **WHEN** asked\n- **THEN** it acts\n"
	it := &Item{ID: "ACME-US-0001", Type: TypeStory, Status: "todo", Body: body}
	tests := []struct {
		name string
		lint *SpecLintConfig
		want []Severity
	}{
		{name: "default warns", want: []Severity{SeverityWarning}},
		{name: "error blocks the write", lint: &SpecLintConfig{Severity: LintError}, want: []Severity{SeverityError}},
		{name: "off is silent", lint: &SpecLintConfig{Severity: LintOff}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := deltaConfig()
			if tt.lint != nil {
				cfg.Specs = &SpecsConfig{Lint: tt.lint}
			}
			var got []Severity
			for _, d := range ValidateItem(it, cfg) {
				if d.Code == LintReqVague {
					got = append(got, d.Severity)
				}
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("LINT-REQ-VAGUE severities = %v, want %v", got, tt.want)
			}
		})
	}
}

// specDeltaVault is a schema-2 project with one spec, stories that propose
// changes to it through Spec Deltas, and a done story whose delta counts as
// applied.
func specDeltaVault(t *testing.T) *Index {
	t.Helper()
	fsys := NewMemFS()
	head := func(id, typ, status string) string {
		return "---\nid: " + id + "\ntype: " + typ + "\ntitle: " + id + "\nstatus: " + status +
			"\ncreated: 2026-01-01T00:00:00Z\nupdated: 2026-01-01T00:00:00Z\n"
	}
	clean := "\nThe system SHALL act.\n\n#### Scenario: acts\n- **WHEN** asked\n- **THEN** it acts\n\n"
	files := map[string]string{
		"docs/.pmngr/project.yaml": "schema: 2\nkey: ACME\nname: Acme\nworkflow:\n  statuses:\n" +
			"    - {id: todo, category: todo}\n    - {id: done, category: done}\n    - {id: cancelled, category: cancelled}\n",
		"docs/.pmngr/specs/ACME-SP-0001-allocation.md": head("ACME-SP-0001", "spec", "todo") +
			"requirements:\n  R1:\n    status: done\n  R2:\n    status: todo\n  R3:\n    status: todo\n---\n\n" +
			"## Requirements\n\n### ACME-SP-0001.R1 — One\n" + clean +
			"### ACME-SP-0001.R2 — Two\n" + clean + "### ACME-SP-0001.R3 — Three\n" + clean,
		"docs/.pmngr/stories/ACME-US-0001-change-it.md": head("ACME-US-0001", "story", "todo") + "---\n\n" +
			"## Spec Delta\n\n" +
			"### MODIFIED ACME-SP-0001.R2 — Two, better\n" + clean +
			"### REMOVED ACME-SP-0001.R3 — Three\n\nReason: folded into R2.\n\n" +
			"### REMOVED ACME-SP-0001.R12 — Deleted by hand\n\nReason: gone.\n\n" +
			"### ADDED ACME-SP-0009 — In a spec nobody wrote\n" + clean +
			"### ADDED ACME-SP-0001 — Moved here\nSupersedes: ACME-SP-0001.R14\n" + clean,
		"docs/.pmngr/stories/ACME-US-0002-done.md": head("ACME-US-0002", "story", "done") + "---\n\n" +
			"## Spec Delta\n\n### MODIFIED ACME-SP-0001.R1 — One, applied\n" + clean,
		"docs/.pmngr/tasks/ACME-T-0001-declared.md": head("ACME-T-0001", "task", "todo") +
			"links:\n  - { kind: modifies, target: ACME-SP-0001.R2 }\n---\n\n" +
			"## Spec Delta\n\n### MODIFIED ACME-SP-0001.R2 — Two, again\n" + clean,
		"docs/.pmngr/tasks/ACME-T-0002-cancelled.md": head("ACME-T-0002", "task", "cancelled") + "---\n\n" +
			"## Spec Delta\n\n### REMOVED ACME-SP-0001.R1 — One\n\nReason: never mind.\n",
	}
	for p, data := range files {
		if err := fsys.MkdirAll(p[:strings.LastIndex(p, "/")]); err != nil {
			t.Fatal(err)
		}
		if err := fsys.WriteFile(p, []byte(data)); err != nil {
			t.Fatal(err)
		}
	}
	projects, err := DiscoverProjects(fsys, ".")
	if err != nil {
		t.Fatal(err)
	}
	ix := NewIndex(fsys, projects)
	if _, err := ix.Build(context.Background(), true); err != nil {
		t.Fatalf("Build(): %v", err)
	}
	return ix
}

func TestIndexIndexesPendingSpecDelta(t *testing.T) {
	t.Parallel()
	ix := specDeltaVault(t)

	tests := []struct {
		name   string
		target string
		kind   LinkKind
		want   []ItemID
	}{
		{name: "modified is pending modifies", target: "ACME-SP-0001.R2", kind: LinkModifiedBy, want: []ItemID{"ACME-T-0001", "ACME-US-0001"}},
		{name: "removed is pending modifies", target: "ACME-SP-0001.R3", kind: LinkModifiedBy, want: []ItemID{"ACME-US-0001"}},
		{name: "done and cancelled deltas propose nothing", target: "ACME-SP-0001.R1", kind: LinkModifiedBy},
		{name: "added proposes no relation", target: "ACME-SP-0009", kind: LinkModifiedBy},
		{name: "what the story modifies", target: "ACME-US-0001", kind: LinkModifies,
			want: []ItemID{"ACME-SP-0001.R12", "ACME-SP-0001.R2", "ACME-SP-0001.R3"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := ix.Related(tt.target, tt.kind); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Related(%s, %s) = %v, want %v", tt.target, tt.kind, got, tt.want)
			}
		})
	}

	for _, l := range ix.LinkGraph().Links("ACME-US-0001") {
		if !l.Pending || l.Computed {
			t.Errorf("link %+v of the story is not a pending declared edge", l)
		}
	}
	// A task that declares the link and also proposes it keeps one edge, the
	// declared one.
	links := ix.LinkGraph().Links("ACME-T-0001")
	if len(links) != 1 || links[0].Pending {
		t.Errorf("links of ACME-T-0001 = %+v, want the declared modifies alone", links)
	}
}

func TestIndexReportsSpecDeltaFindings(t *testing.T) {
	t.Parallel()
	ix := specDeltaVault(t)

	var got []string
	for _, d := range ix.Warnings() {
		if strings.HasPrefix(string(d.Code), "W-DELTA") || strings.HasPrefix(string(d.Code), "E-DELTA") {
			got = append(got, fmt.Sprintf("%s %s %s", filepath.Base(d.Path), d.Code, d.Field))
		}
	}
	sort.Strings(got)
	want := []string{
		"ACME-US-0001-change-it.md W-DELTA-DANGLING body.ACME-SP-0001",
		"ACME-US-0001-change-it.md W-DELTA-DANGLING body.ACME-SP-0001.R12",
		"ACME-US-0001-change-it.md W-DELTA-DANGLING body.ACME-SP-0009",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Spec Delta diagnostics =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestIndexSpecDeltaReservesRequirementNumbers(t *testing.T) {
	t.Parallel()
	ix := specDeltaVault(t)

	// R12 (a REMOVED target) and R14 (a Supersedes: line) are held by the
	// delta alone; the ADDED blocks take no number until the story is done.
	n, err := ix.NextRequirementNumber("ACME-SP-0001")
	if err != nil {
		t.Fatalf("NextRequirementNumber(): %v", err)
	}
	if n != 15 {
		t.Errorf("NextRequirementNumber() = %d, want 15", n)
	}
	var nums []int
	for _, r := range ix.RequirementRefsTo("ACME-SP-0001") {
		nums = append(nums, r.Number)
	}
	if want := []int{1, 2, 3, 12, 14}; !reflect.DeepEqual(nums, want) {
		t.Errorf("RequirementRefsTo() numbers = %v, want %v", nums, want)
	}
}

func TestLintSpecDeltaAndProposedText(t *testing.T) {
	t.Parallel()

	body := "## Spec Delta\n\n" +
		"### ADDED ACME-SP-0003 — Reject fast input\n\nSupersedes: ACME-SP-0003.R1\n\n" +
		"The form SHALL reject input fast.\n\n" +
		"#### Scenario: typed\n- **WHEN** a person types\n- **THEN** it is rejected\n\n" +
		"### MODIFIED ACME-SP-0003.R2 — Trim\n\nthe form trims input.\n\n" +
		"### REMOVED ACME-SP-0003.R4 — Gone\n\nReason: nobody used it.\n"
	delta := ParseSpecDelta(body)
	if len(delta.Operations) != 3 {
		t.Fatalf("operations = %d, want 3", len(delta.Operations))
	}

	tests := []struct {
		name string
		cfg  *SpecLintConfig
		want []string
	}{
		{
			name: "default levels",
			cfg:  nil,
			want: []string{
				"LINT-REQ-VAGUE warning 7",
				"LINT-REQ-STATEMENT warning 15",
				"LINT-REQ-SCENARIO warning 13",
			},
		},
		{
			name: "vague is an error, the scenario rule is off",
			cfg: &SpecLintConfig{Severity: LintWarning, Rules: map[Code]LintLevel{
				LintReqVague: LintError, LintReqScenario: LintOff,
			}},
			want: []string{"LINT-REQ-VAGUE error 7", "LINT-REQ-STATEMENT warning 15"},
		},
		{name: "everything off", cfg: &SpecLintConfig{Severity: LintOff}, want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var got []string
			for _, f := range LintSpecDelta(delta, tt.cfg) {
				got = append(got, fmt.Sprintf("%s %s %d", f.Rule, f.Severity, f.Line))
			}
			sort.Strings(got)
			want := append([]string(nil), tt.want...)
			sort.Strings(want)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("findings = %q, want %q", got, want)
			}
		})
	}

	proposed := []string{
		"The form SHALL reject input fast.\n\n#### Scenario: typed\n- **WHEN** a person types\n- **THEN** it is rejected",
		"the form trims input.",
		"",
	}
	for i, op := range delta.Operations {
		if got := op.ProposedText(); got != proposed[i] {
			t.Errorf("%s ProposedText() = %q, want %q", op.Op, got, proposed[i])
		}
	}
}
