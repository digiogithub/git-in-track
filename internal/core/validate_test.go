package core

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// testConfig loads the fixture project of testdata/validate, which declares one
// unreachable status, two custom fields, two labels and one person.
func testConfig(t *testing.T) *ProjectConfig {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "validate", "project.yaml"))
	if err != nil {
		t.Fatalf("read fixture project: %v", err)
	}
	cfg, err := LoadProjectConfig(data)
	if err != nil {
		t.Fatalf("LoadProjectConfig(): %v", err)
	}
	return cfg
}

// mustTimestamp parses a canonical timestamp or fails the test.
func mustTimestamp(t *testing.T, s string) Timestamp {
	t.Helper()
	ts, err := ParseTimestamp(s)
	if err != nil {
		t.Fatalf("ParseTimestamp(%q): %v", s, err)
	}
	return ts
}

// validStory returns an item that must validate without a single diagnostic.
func validStory(t *testing.T) *Item {
	t.Helper()
	estimate := 5.0
	return &Item{
		ID:        "TEST-US-0001",
		Type:      TypeStory,
		Title:     "A clean story",
		Status:    "in_progress",
		Priority:  PriorityHigh,
		Parent:    "TEST-EP-0002",
		Milestone: "TEST-M-0001",
		Assignees: []string{"jose"},
		Author:    "jose",
		Labels:    []string{"core"},
		Estimate:  &estimate,
		Created:   mustTimestamp(t, "2026-01-05T09:00:00Z"),
		Updated:   mustTimestamp(t, "2026-02-01T10:30:00Z"),
		Started:   mustTimestamp(t, "2026-01-20T08:00:00Z"),
		Links:     []Link{{Kind: LinkBlockedBy, Target: "TEST-T-0002"}},
		Custom:    map[string]any{"risk": "medium"},
		Body:      "## Description\n\nBody.",
		Path:      "docs/.pmngr/stories/TEST-US-0001-a-clean-story.md",
	}
}

// codesOf projects diagnostics onto their codes, keeping the order they were
// reported in so that a test also pins the ordering contract.
func codesOf(diags []Diagnostic) []Code {
	out := make([]Code, 0, len(diags))
	for _, d := range diags {
		out = append(out, d.Code)
	}
	return out
}

// sortedCodes is codesOf for cases where only the set matters.
func sortedCodes(diags []Diagnostic) []Code {
	out := codesOf(diags)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func TestValidateItemRules(t *testing.T) {
	t.Parallel()
	cfg := testConfig(t)

	tests := []struct {
		name   string
		mutate func(*Item)
		want   []Code
	}{
		{name: "a valid story reports nothing", mutate: func(*Item) {}},
		{
			name:   "missing type",
			mutate: func(it *Item) { it.Type = "" },
			want:   []Code{CodeFMType},
		},
		{
			name:   "unknown type",
			mutate: func(it *Item) { it.Type = "spike" },
			want:   []Code{CodeFMType},
		},
		{
			name:   "a comment is not an item",
			mutate: func(it *Item) { it.Type = TypeComment },
			want:   []Code{CodeFMType},
		},
		{
			name:   "type does not match the folder",
			mutate: func(it *Item) { it.Path = "docs/.pmngr/tasks/TEST-US-0001-a-clean-story.md" },
			want:   []Code{CodeFMType},
		},
		{
			name:   "missing id",
			mutate: func(it *Item) { it.ID = "" },
			want:   []Code{CodeIDMissing},
		},
		{
			name:   "id grammar",
			mutate: func(it *Item) { it.ID = "test-us-1" },
			want:   []Code{CodeIDFilename, CodeIDGrammar},
		},
		{
			name:   "id of another project",
			mutate: func(it *Item) { it.ID, it.Path = "OTHER-US-0001", "" },
			want:   []Code{CodeIDKey},
		},
		{
			name:   "type code does not match the type",
			mutate: func(it *Item) { it.ID, it.Path = "TEST-T-0001", "" },
			want:   []Code{CodeIDTypeCode},
		},
		{
			name:   "file name claims another id",
			mutate: func(it *Item) { it.Path = "docs/.pmngr/stories/TEST-US-0099-a-clean-story.md" },
			want:   []Code{CodeIDFilename},
		},
		{
			name:   "file name without an id",
			mutate: func(it *Item) { it.Path = "docs/.pmngr/stories/notes.md" },
			want:   []Code{CodeIDFilename},
		},
		{
			name:   "stale slug is only a warning",
			mutate: func(it *Item) { it.Title = "A renamed story" },
			want:   []Code{CodeWarnSlugStale},
		},
		{
			name:   "missing title",
			mutate: func(it *Item) { it.Title = "   " },
			want:   []Code{CodeTitle, CodeWarnSlugStale},
		},
		{
			name:   "title longer than 200 characters",
			mutate: func(it *Item) { it.Title = strings.Repeat("x", 201) },
			want:   []Code{CodeTitle, CodeWarnSlugStale},
		},
		{
			name:   "missing created and updated",
			mutate: func(it *Item) { it.Created, it.Updated = Timestamp{}, Timestamp{} },
			want:   []Code{CodeFieldRequired, CodeFieldRequired},
		},
		{
			name:   "missing status",
			mutate: func(it *Item) { it.Status = "" },
			want:   []Code{CodeFieldRequired},
		},
		{
			name:   "unknown status",
			mutate: func(it *Item) { it.Status = "shipped" },
			want:   []Code{CodeStatusUnknown},
		},
		{
			name:   "status unreachable through the declared transitions",
			mutate: func(it *Item) { it.Status = "archived" },
			want:   []Code{CodeWarnWorkflowTransition},
		},
		{
			name:   "unknown priority",
			mutate: func(it *Item) { it.Priority = "urgent" },
			want:   []Code{CodeEnum},
		},
		{
			name:   "a story hangs from an epic",
			mutate: func(it *Item) { it.Parent = "TEST-T-0002" },
			want:   []Code{CodeRefParentType},
		},
		{
			name:   "an epic has no parent",
			mutate: func(it *Item) { asEpic(it) },
			want:   []Code{CodeRefParentType},
		},
		{
			name: "a task hangs from a story or an epic",
			mutate: func(it *Item) {
				asTask(it)
				it.Parent = "TEST-M-0001"
			},
			want: []Code{CodeRefParentType},
		},
		{
			name: "a task may hang from an epic",
			mutate: func(it *Item) {
				asTask(it)
				it.Parent = "TEST-EP-0002"
			},
		},
		{
			name:   "a milestone has no parent",
			mutate: func(it *Item) { asMilestone(it) },
			want:   []Code{CodeRefParentType},
		},
		{
			name:   "parent grammar",
			mutate: func(it *Item) { it.Parent = "EP-1" },
			want:   []Code{CodeIDGrammar},
		},
		{
			name:   "parent of another project",
			mutate: func(it *Item) { it.Parent = "OTHER-EP-0001" },
			want:   []Code{CodeIDKey},
		},
		{
			name:   "an item cannot be its own parent",
			mutate: func(it *Item) { it.Parent = it.ID },
			want:   []Code{CodeRefCycle},
		},
		{
			name:   "the epic alias must name an epic",
			mutate: func(it *Item) { it.Epic = "TEST-US-0007" },
			want:   []Code{CodeRefTargetType},
		},
		{
			name:   "milestone must name a milestone",
			mutate: func(it *Item) { it.Milestone = "TEST-EP-0002" },
			want:   []Code{CodeRefTargetType},
		},
		{
			name:   "milestone of another project",
			mutate: func(it *Item) { it.Milestone = "OTHER-M-0001" },
			want:   []Code{CodeIDKey},
		},
		{
			name:   "unknown link kind",
			mutate: func(it *Item) { it.Links = []Link{{Kind: "supersedes", Target: "TEST-US-0002"}} },
			want:   []Code{CodeEnum},
		},
		{
			name:   "a relation needs a target",
			mutate: func(it *Item) { it.Links = []Link{{Kind: LinkBlocks}} },
			want:   []Code{CodeFieldType},
		},
		{
			name:   "a relation needs a kind",
			mutate: func(it *Item) { it.Links = []Link{{Target: "TEST-US-0002"}} },
			want:   []Code{CodeFieldType},
		},
		{
			name:   "link target grammar",
			mutate: func(it *Item) { it.Links = []Link{{Kind: LinkBlocks, Target: "US-2"}} },
			want:   []Code{CodeIDGrammar},
		},
		{
			name:   "a bare link target implies this project",
			mutate: func(it *Item) { it.Links = []Link{{Kind: LinkRelatesTo, Target: "OTHER-US-0031"}} },
			want:   []Code{CodeIDKey},
		},
		{
			name:   "a qualified link target crosses projects",
			mutate: func(it *Item) { it.Links = []Link{{Kind: LinkRelatesTo, Target: "OTHER/OTHER-US-0031"}} },
		},
		{
			name:   "a qualifier that disagrees with the id",
			mutate: func(it *Item) { it.Links = []Link{{Kind: LinkRelatesTo, Target: "OTHER/TEST-US-0031"}} },
			want:   []Code{CodeIDKey},
		},
		{
			name:   "a qualifier that is not a project key",
			mutate: func(it *Item) { it.Links = []Link{{Kind: LinkRelatesTo, Target: "other/OTHER-US-0031"}} },
			want:   []Code{CodeIDGrammar},
		},
		{
			name:   "closed before started",
			mutate: func(it *Item) { it.Closed = mustTimestamp(t, "2026-01-06T09:00:00Z") },
			want:   []Code{CodeDateOrder},
		},
		{
			name:   "started before created",
			mutate: func(it *Item) { it.Started = mustTimestamp(t, "2026-01-01T09:00:00Z") },
			want:   []Code{CodeDateOrder},
		},
		{
			name:   "updated before created",
			mutate: func(it *Item) { it.Updated = mustTimestamp(t, "2026-01-04T09:00:00Z") },
			want:   []Code{CodeDateOrder},
		},
		{
			name: "due before start",
			mutate: func(it *Item) {
				it.Start = mustDate(t, "2026-06-01")
				it.Due = mustDate(t, "2026-05-01")
			},
			want: []Code{CodeDateOrder},
		},
		{
			name: "a timestamp that is not UTC with second precision",
			mutate: func(it *Item) {
				it.Updated = Timestamp{Time: it.Updated.Add(500)}
			},
			want: []Code{CodeDateFormat},
		},
		{
			name:   "estimate outside the scale is a warning",
			mutate: func(it *Item) { it.Estimate = ptr(4.0) },
			want:   []Code{CodeWarnEstimateScale},
		},
		{
			name:   "a negative estimate",
			mutate: func(it *Item) { it.Estimate = ptr(-1.0) },
			want:   []Code{CodeFieldType, CodeWarnEstimateScale},
		},
		{
			name:   "an undeclared label is a warning",
			mutate: func(it *Item) { it.Labels = []string{"core", "marketing"} },
			want:   []Code{CodeWarnLabelUndeclared},
		},
		{
			name:   "an unknown handle is a warning",
			mutate: func(it *Item) { it.Author, it.Assignees = "nobody", []string{"jose", "ghost"} },
			want:   []Code{CodeWarnPersonUnknown, CodeWarnPersonUnknown},
		},
		{
			name:   "an undeclared custom key is a warning",
			mutate: func(it *Item) { it.Custom["sprint_goal"] = "ship it" },
			want:   []Code{CodeWarnCustomUndeclared},
		},
		{
			name:   "a custom key declared for another type",
			mutate: func(it *Item) { it.Custom["phase"] = 3 },
			want:   []Code{CodeWarnCustomUndeclared},
		},
		{
			name:   "a custom enum outside its value set",
			mutate: func(it *Item) { it.Custom["risk"] = "extreme" },
			want:   []Code{CodeCustomFieldType},
		},
		{
			name: "a custom number holding a string",
			mutate: func(it *Item) {
				asMilestone(it)
				it.Parent = ""
				it.Custom = map[string]any{"phase": "one"}
			},
			want: []Code{CodeCustomFieldType},
		},
		{
			name: "several problems are reported in one pass",
			mutate: func(it *Item) {
				it.Status = "shipped"
				it.Parent = "TEST-T-0002"
				it.Labels = []string{"marketing"}
				it.Title = "A renamed story"
			},
			want: []Code{CodeRefParentType, CodeStatusUnknown, CodeWarnLabelUndeclared, CodeWarnSlugStale},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			item := validStory(t)
			tt.mutate(item)
			got := sortedCodes(ValidateItem(item, cfg))
			want := tt.want
			if want == nil {
				want = []Code{}
			}
			sort.Slice(want, func(i, j int) bool { return want[i] < want[j] })
			if !reflect.DeepEqual(got, want) {
				t.Errorf("codes = %v, want %v\ndiagnostics:\n%s", got, want, render(ValidateItem(item, cfg)))
			}
		})
	}
}

func TestValidateItemWithoutAProject(t *testing.T) {
	t.Parallel()

	item := validStory(t)
	item.Status = "whatever-the-project-declares"
	item.Custom["sprint_goal"] = "ship it"
	item.Labels = []string{"unknown-label"}
	if diags := ValidateItem(item, nil); len(diags) != 0 {
		t.Errorf("ValidateItem(item, nil) = %s, want no diagnostic", render(diags))
	}

	broken := validStory(t)
	broken.ID = "nope"
	broken.Path = ""
	if got := sortedCodes(ValidateItem(broken, nil)); !reflect.DeepEqual(got, []Code{CodeIDGrammar}) {
		t.Errorf("codes = %v, want [E-ID-GRAMMAR]", got)
	}

	if diags := ValidateItem(nil, nil); diags != nil {
		t.Errorf("ValidateItem(nil, nil) = %v, want nil", diags)
	}
}

func TestValidateItemOrderIsDeterministic(t *testing.T) {
	t.Parallel()
	cfg := testConfig(t)

	item := validStory(t)
	item.Title = "A renamed story"
	item.Status = "shipped"
	item.Author = "nobody"
	item.Labels = []string{"marketing", "sales"}
	item.Parent = "TEST-T-0002"
	item.Custom["sprint_goal"] = "ship it"
	item.Custom["risk"] = "extreme"

	first := ValidateItem(item, cfg)
	for i := 0; i < 20; i++ {
		if got := ValidateItem(item, cfg); !reflect.DeepEqual(got, first) {
			t.Fatalf("run %d differs:\n%s\nwant:\n%s", i, render(got), render(first))
		}
	}

	// Errors come first, then warnings; inside a severity, codes ascend.
	seenWarning := false
	for i, d := range first {
		if d.Severity == SeverityWarning {
			seenWarning = true
		} else if seenWarning {
			t.Fatalf("diagnostic %d (%s) is an error after a warning:\n%s", i, d.Code, render(first))
		}
		if i > 0 && first[i-1].Severity == d.Severity && first[i-1].Code > d.Code {
			t.Fatalf("codes are not ascending at %d:\n%s", i, render(first))
		}
	}
	if !HasErrors(first) {
		t.Error("HasErrors() = false, want true")
	}
	if HasErrors(ValidateItem(validStory(t), cfg)) {
		t.Error("HasErrors() = true for a valid story")
	}

	// Every error of the file is reported at once, joined as errors.Join does.
	err := JoinDiagnostics(first)
	if err == nil {
		t.Fatal("JoinDiagnostics() = nil, want the errors of the run")
	}
	joined, ok := err.(interface{ Unwrap() []error })
	if !ok {
		t.Fatalf("JoinDiagnostics() = %T, want a joined error", err)
	}
	wantErrors := 0
	for _, d := range first {
		if d.Severity == SeverityError {
			wantErrors++
		}
	}
	if got := len(joined.Unwrap()); got != wantErrors {
		t.Errorf("joined %d errors, want %d", got, wantErrors)
	}
	var diagErr *DiagnosticError
	if !errors.As(err, &diagErr) {
		t.Error("errors.As(*DiagnosticError) = false")
	} else if diagErr.Diagnostic.Path == "" || diagErr.Diagnostic.Field == "" {
		t.Errorf("diagnostic error without a path or a field: %#v", diagErr.Diagnostic)
	}
	if JoinDiagnostics(ValidateItem(validStory(t), cfg)) != nil {
		t.Error("JoinDiagnostics() on a valid story must be nil")
	}
}

func TestValidateItemFixtures(t *testing.T) {
	t.Parallel()
	cfg := testConfig(t)

	tests := []struct {
		name string
		file string
		want []Code
	}{
		{
			name: "a clean story",
			file: "stories/TEST-US-0001-a-clean-story.md",
		},
		{
			name: "unknown status",
			file: "stories/TEST-US-0002-unknown-status.md",
			want: []Code{CodeStatusUnknown},
		},
		{
			name: "parent of the wrong type",
			file: "stories/TEST-US-0003-parent-of-the-wrong-type.md",
			want: []Code{CodeRefParentType, CodeRefTargetType},
		},
		{
			name: "an outdated slug",
			file: "stories/TEST-US-0004-an-outdated-slug.md",
			want: []Code{CodeWarnSlugStale},
		},
		{
			name: "labels and custom fields",
			file: "stories/TEST-US-0005-labels-and-custom-fields.md",
			want: []Code{
				CodeCustomFieldType,
				CodeWarnCustomUndeclared,
				CodeWarnEstimateScale,
				CodeWarnLabelUndeclared,
				CodeWarnPersonUnknown,
				CodeWarnWorkflowTransition,
			},
		},
		{
			name: "an epic with a parent",
			file: "epics/TEST-EP-0001-an-epic-with-a-parent.md",
			want: []Code{CodeRefParentType},
		},
		{
			name: "references to another project",
			file: "tasks/TEST-T-0001-references-to-another-project.md",
			want: []Code{CodeIDKey, CodeIDKey, CodeIDKey},
		},
		{
			name: "a story in the tasks folder",
			file: "tasks/TEST-US-0009-a-story-in-the-tasks-folder.md",
			want: []Code{CodeFMType},
		},
		{
			name: "dates out of order",
			file: "milestones/TEST-M-0001-dates-out-of-order.md",
			want: []Code{CodeCustomFieldType, CodeDateOrder, CodeDateOrder, CodeDateOrder, CodeDateOrder},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join("testdata", "validate", filepath.FromSlash(tt.file))
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			item, err := ParseItem(tt.file, data)
			if err != nil {
				t.Fatalf("ParseItem(): %v", err)
			}
			diags := ValidateItem(item, cfg)
			got := sortedCodes(diags)
			want := tt.want
			if want == nil {
				want = []Code{}
			}
			sort.Slice(want, func(i, j int) bool { return want[i] < want[j] })
			if !reflect.DeepEqual(got, want) {
				t.Errorf("codes = %v, want %v\ndiagnostics:\n%s", got, want, render(diags))
			}
		})
	}
}

func TestValidateTransition(t *testing.T) {
	t.Parallel()
	cfg := testConfig(t)

	tests := []struct {
		name       string
		from, to   Status
		wantCode   Code
		wantSevere Severity
	}{
		{name: "a declared transition", from: "todo", to: "in_progress"},
		{name: "the same status", from: "done", to: "done"},
		{name: "a new item has no origin", to: "backlog"},
		{name: "an undeclared transition", from: "backlog", to: "done", wantCode: CodeWarnWorkflowTransition, wantSevere: SeverityWarning},
		{name: "a status with no outgoing transition", from: "archived", to: "todo", wantCode: CodeWarnWorkflowTransition, wantSevere: SeverityWarning},
		{name: "an unknown target status", from: "todo", to: "shipped", wantCode: CodeStatusUnknown, wantSevere: SeverityError},
		{name: "an unknown origin status", from: "shipped", to: "todo", wantCode: CodeStatusUnknown, wantSevere: SeverityError},
		{name: "no target status at all", from: "todo", wantCode: CodeFieldRequired, wantSevere: SeverityError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ValidateTransition(cfg, tt.from, tt.to)
			if got.Code != tt.wantCode {
				t.Fatalf("ValidateTransition(%q, %q) = %q, want %q", tt.from, tt.to, got.Code, tt.wantCode)
			}
			if got.Severity != tt.wantSevere {
				t.Errorf("severity = %q, want %q", got.Severity, tt.wantSevere)
			}
			if allowed := TransitionAllowed(cfg, tt.from, tt.to); allowed != (tt.wantCode == "") {
				t.Errorf("TransitionAllowed() = %t, want %t", allowed, tt.wantCode == "")
			}
		})
	}

	// A workflow without transitions allows every move between declared statuses.
	open := *cfg
	open.Workflow.Transitions = nil
	if d := ValidateTransition(&open, "backlog", "done"); d.Code != "" {
		t.Errorf("open workflow: %s", d)
	}
	if d := ValidateTransition(nil, "backlog", "done"); d.Code != "" {
		t.Errorf("nil config: %s", d)
	}
}

func TestValidateProject(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*ProjectConfig)
		want   []Code
	}{
		{name: "the fixture project is valid", mutate: func(*ProjectConfig) {}},
		{
			name:   "a renamed priority",
			mutate: func(cfg *ProjectConfig) { cfg.Priorities = append(cfg.Priorities, "urgent") },
			want:   []Code{CodeEnum},
		},
		{
			name:   "an unknown estimation scale",
			mutate: func(cfg *ProjectConfig) { cfg.Estimation.Scale = "hours" },
			want:   []Code{CodeEnum},
		},
		{
			name: "a custom field without a key and with an unknown type",
			mutate: func(cfg *ProjectConfig) {
				cfg.CustomFields = append(cfg.CustomFields, CustomField{Type: "hue"})
			},
			want: []Code{CodeEnum, CodeFieldRequired},
		},
		{
			name: "an enum custom field without values",
			mutate: func(cfg *ProjectConfig) {
				cfg.CustomFields = append(cfg.CustomFields, CustomField{Key: "risk2", Type: "enum"})
			},
			want: []Code{CodeFieldRequired},
		},
		{
			name: "a list custom field with an unknown element type",
			mutate: func(cfg *ProjectConfig) {
				cfg.CustomFields = append(cfg.CustomFields, CustomField{Key: "reviewers", Type: "list", Items: "hue"})
			},
			want: []Code{CodeEnum},
		},
		{
			name: "applies_to names an unknown type",
			mutate: func(cfg *ProjectConfig) {
				cfg.CustomFields = append(cfg.CustomFields, CustomField{Key: "spike", Type: "bool", AppliesTo: []ItemType{"board"}})
			},
			want: []Code{CodeEnum},
		},
		{
			name: "a default status that is not declared",
			mutate: func(cfg *ProjectConfig) {
				cfg.Defaults[TypeTask] = ItemDefaults{Status: "queued", Priority: "urgent", Labels: []string{"marketing"}}
			},
			want: []Code{CodeEnum, CodeStatusUnknown, CodeWarnLabelUndeclared},
		},
		{
			name: "an unknown status category",
			mutate: func(cfg *ProjectConfig) {
				cfg.Workflow.Statuses = append(cfg.Workflow.Statuses, StatusDef{ID: "paused", Category: "waiting"})
			},
			want: []Code{CodeProjStatusCategory},
		},
		{
			name:   "an initial status that is not declared",
			mutate: func(cfg *ProjectConfig) { cfg.Workflow.Initial = "queued" },
			want:   []Code{CodeProjInitial},
		},
		{
			name:   "a project key that does not match the grammar",
			mutate: func(cfg *ProjectConfig) { cfg.Key = "test" },
			want:   []Code{CodeProjKey},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := testConfig(t)
			tt.mutate(cfg)
			got := sortedCodes(ValidateProject(cfg))
			want := tt.want
			if want == nil {
				want = []Code{}
			}
			sort.Slice(want, func(i, j int) bool { return want[i] < want[j] })
			if !reflect.DeepEqual(got, want) {
				t.Errorf("codes = %v, want %v\ndiagnostics:\n%s", got, want, render(ValidateProject(cfg)))
			}
		})
	}

	if got := codesOf(ValidateProject(nil)); !reflect.DeepEqual(got, []Code{CodeProjMissing}) {
		t.Errorf("ValidateProject(nil) = %v, want [E-PROJ-MISSING]", got)
	}
}

// TestValidateVaults is the acceptance test of the story: every file of the two
// backlogs this repository ships — the fixture project and its own dogfooded
// docs/.pmngr — must validate without a single error. Warnings are allowed and
// printed, because a warning is exactly what the data model says must never
// block a read.
func TestValidateVaults(t *testing.T) {
	t.Parallel()

	vaults := []struct {
		name string
		dir  string
	}{
		{name: "fixture project-basic", dir: filepath.Join("..", "..", "testdata", "fixtures", "project-basic", "docs", ".pmngr")},
		{name: "dogfood docs/.pmngr", dir: filepath.Join("..", "..", "docs", ".pmngr")},
	}

	for _, v := range vaults {
		t.Run(v.name, func(t *testing.T) {
			t.Parallel()

			data, err := os.ReadFile(filepath.Join(v.dir, ProjectFileName))
			if err != nil {
				t.Fatalf("read project: %v", err)
			}
			cfg, err := LoadProjectConfig(data)
			if err != nil {
				t.Fatalf("LoadProjectConfig(): %v", err)
			}
			for _, d := range ValidateProject(cfg) {
				report(t, d)
			}

			files := backlogFiles(t, v.dir)
			if len(files) == 0 {
				t.Fatalf("no markdown file found under %s", v.dir)
			}
			for _, rel := range files {
				raw, err := os.ReadFile(filepath.Join(v.dir, filepath.FromSlash(rel)))
				if err != nil {
					t.Fatalf("read %s: %v", rel, err)
				}
				if strings.HasPrefix(rel, "comments/") {
					if _, err := ParseComment(rel, raw); err != nil {
						t.Errorf("ParseComment(%s): %v", rel, err)
					}
					continue
				}
				item, err := ParseItem(rel, raw)
				if err != nil {
					t.Errorf("ParseItem(%s): %v", rel, err)
					continue
				}
				for _, d := range ValidateItem(item, cfg) {
					report(t, d)
				}
			}
		})
	}
}

// report fails the test on an error and logs a warning.
func report(t *testing.T, d Diagnostic) {
	t.Helper()
	if d.Severity == SeverityError {
		t.Errorf("error: %s", d)
		return
	}
	t.Logf("warning: %s", d)
}

// backlogFiles lists the vault-relative paths of every Markdown file under dir,
// sorted, so the walk order does not depend on the file system.
func backlogFiles(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
	sort.Strings(out)
	return out
}

// render prints diagnostics one per line for a failure message.
func render(diags []Diagnostic) string {
	var b strings.Builder
	for _, d := range diags {
		b.WriteString("  ")
		b.WriteString(d.String())
		b.WriteString("\n")
	}
	return b.String()
}

// ptr returns a pointer to a float, for the nil-able numeric fields.
func ptr(f float64) *float64 { return &f }

// mustDate parses a date or fails the test.
func mustDate(t *testing.T, s string) Date {
	t.Helper()
	d, err := ParseDate(s)
	if err != nil {
		t.Fatalf("ParseDate(%q): %v", s, err)
	}
	return d
}

// asEpic, asTask and asMilestone retype the valid story so that one table can
// exercise the parent rules of every item type.
func asEpic(it *Item) {
	it.Type, it.ID = TypeEpic, "TEST-EP-0001"
	it.Path, it.Custom = "", nil
}

func asTask(it *Item) {
	it.Type, it.ID = TypeTask, "TEST-T-0001"
	it.Path, it.Custom = "", nil
}

func asMilestone(it *Item) {
	it.Type, it.ID = TypeMilestone, "TEST-M-0001"
	it.Path, it.Custom, it.Milestone = "", nil, ""
}
