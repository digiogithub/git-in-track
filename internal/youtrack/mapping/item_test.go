package mapping

import (
	"testing"

	"github.com/digiogithub/git-in-track/internal/core"
	"github.com/digiogithub/git-in-track/internal/youtrack"
)

func TestMapType(t *testing.T) {
	cases := []struct {
		name     string
		value    string
		fieldMap FieldMap
		want     core.ItemType
		warns    bool
	}{
		{name: "epic", value: `{"name":"Epic","$type":"EnumBundleElement"}`, want: core.TypeEpic},
		{name: "user story", value: `{"name":"User Story","$type":"EnumBundleElement"}`, want: core.TypeStory},
		{name: "task", value: `{"name":"Task","$type":"EnumBundleElement"}`, want: core.TypeTask},
		{name: "bug becomes a task", value: `{"name":"Bug","$type":"EnumBundleElement"}`, want: core.TypeTask},
		{name: "case and spacing do not matter", value: `{"name":"  user STORY ","$type":"EnumBundleElement"}`, want: core.TypeStory},
		{name: "a version bundle value is a milestone", value: `{"name":"1.5.0","$type":"VersionBundleElement"}`, want: core.TypeMilestone},
		{name: "an absent field takes the default silently", value: `null`, want: core.TypeTask},
		{name: "an unknown value warns and falls back", value: `{"name":"Spike","$type":"EnumBundleElement"}`, want: core.TypeTask, warns: true},
		{
			name:     "the field map overrides the built-in default",
			value:    `{"name":"Spike","$type":"EnumBundleElement"}`,
			fieldMap: FieldMap{Types: map[string]core.ItemType{"spike": core.TypeStory}},
			want:     core.TypeStory,
		},
		{
			name:     "a field map entry that is not an item type warns",
			value:    `{"name":"Spike","$type":"EnumBundleElement"}`,
			fieldMap: FieldMap{Types: map[string]core.ItemType{"spike": core.ItemType("saga")}},
			want:     core.TypeTask,
			warns:    true,
		},
		{
			name:     "a renamed type field is read",
			value:    `{"name":"Epic","$type":"EnumBundleElement"}`,
			fieldMap: FieldMap{TypeField: "Tipo"},
			want:     core.TypeEpic,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := tc.fieldMap.withDefaults()
			issue := issueWithField(m.TypeField, tc.value)
			got, warnings := mapType(issue, m)
			if got != tc.want {
				t.Errorf("got type %q, want %q", got, tc.want)
			}
			if warned := len(warnings) > 0; warned != tc.warns {
				t.Errorf("got warnings %v, want warned=%v", warningLines(warnings), tc.warns)
			}
		})
	}
}

func TestMapStatus(t *testing.T) {
	cases := []struct {
		name     string
		value    string
		fieldMap FieldMap
		want     core.Status
		warns    bool
	}{
		{name: "submitted is the backlog", value: `{"name":"Submitted"}`, want: core.Status("backlog")},
		{name: "open is todo", value: `{"name":"Open"}`, want: core.Status("todo")},
		{name: "in progress", value: `{"name":"In Progress"}`, want: core.Status("in_progress")},
		{name: "to verify is in review", value: `{"name":"To Verify"}`, want: core.Status("in_review")},
		{name: "fixed is done", value: `{"name":"Fixed"}`, want: core.Status("done")},
		{name: "won't fix is cancelled", value: `{"name":"Won't fix"}`, want: core.Status("cancelled")},
		{name: "an unknown state warns and leaves the project default", value: `{"name":"Parked"}`, want: core.Status(""), warns: true},
		{
			name:     "a project map replaces the defaults wholesale",
			value:    `{"name":"Parked"}`,
			fieldMap: FieldMap{Statuses: map[string]core.Status{"parked": core.Status("backlog")}},
			want:     core.Status("backlog"),
		},
		{
			name:     "an unknown state can fall back to a configured status",
			value:    `{"name":"Parked"}`,
			fieldMap: FieldMap{DefaultStatus: core.Status("triage")},
			want:     core.Status("triage"),
			warns:    true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := tc.fieldMap.withDefaults()
			got, warnings := mapStatus(issueWithField(m.StateField, tc.value), m)
			if got != tc.want {
				t.Errorf("got status %q, want %q", got, tc.want)
			}
			if warned := len(warnings) > 0; warned != tc.warns {
				t.Errorf("got warnings %v, want warned=%v", warningLines(warnings), tc.warns)
			}
		})
	}
}

func TestMapPriority(t *testing.T) {
	cases := []struct {
		name  string
		value string
		want  core.Priority
		warns bool
	}{
		{name: "show-stopper is critical", value: `{"name":"Show-stopper"}`, want: core.PriorityCritical},
		{name: "critical", value: `{"name":"Critical"}`, want: core.PriorityCritical},
		{name: "major is high", value: `{"name":"Major"}`, want: core.PriorityHigh},
		{name: "normal is medium", value: `{"name":"Normal"}`, want: core.PriorityMedium},
		{name: "minor is low", value: `{"name":"Minor"}`, want: core.PriorityLow},
		{name: "an absent field is medium, silently", value: `null`, want: core.PriorityMedium},
		{name: "an unknown priority warns and falls back to medium", value: `{"name":"Nice to have"}`, want: core.PriorityMedium, warns: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := DefaultFieldMap()
			got, warnings := mapPriority(issueWithField(m.PriorityField, tc.value), m)
			if got != tc.want {
				t.Errorf("got priority %q, want %q", got, tc.want)
			}
			if warned := len(warnings) > 0; warned != tc.warns {
				t.Errorf("got warnings %v, want warned=%v", warningLines(warnings), tc.warns)
			}
		})
	}
}

func TestMapEstimate(t *testing.T) {
	cases := []struct {
		name     string
		value    string
		fieldMap FieldMap
		want     *float64
		warns    bool
	}{
		{name: "minutes win over the presentation", value: `{"presentation":"1w","minutes":480}`, want: points(1)},
		{name: "a day is one point", value: `{"presentation":"1d"}`, want: points(1)},
		{name: "hours and days add up", value: `{"presentation":"3d 4h"}`, want: points(3.5)},
		{name: "a week is five days", value: `{"presentation":"2w"}`, want: points(10)},
		{name: "minutes round to two decimals", value: `{"presentation":"90m"}`, want: points(0.19)},
		{name: "an absent field sets no estimate", value: `null`, want: nil},
		{name: "an unreadable presentation warns and sets no estimate", value: `{"presentation":"about a fortnight"}`, want: nil, warns: true},
		{name: "a presentation with stray text is not guessed at", value: `{"presentation":"3d-ish"}`, want: nil, warns: true},
		{
			name:     "the points-per-day ratio is configurable",
			value:    `{"presentation":"1d"}`,
			fieldMap: FieldMap{MinutesPerPoint: 60},
			want:     points(8),
		},
		{
			name:     "a six-hour working day is configurable",
			value:    `{"presentation":"1d"}`,
			fieldMap: FieldMap{MinutesPerDay: 360, MinutesPerPoint: 360},
			want:     points(1),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := tc.fieldMap.withDefaults()
			got, warnings := mapEstimate(issueWithField(m.EstimationField, tc.value), m)
			switch {
			case tc.want == nil && got != nil:
				t.Errorf("got estimate %v, want none", *got)
			case tc.want != nil && got == nil:
				t.Errorf("got no estimate, want %v", *tc.want)
			case tc.want != nil && *got != *tc.want:
				t.Errorf("got estimate %v, want %v", *got, *tc.want)
			}
			if warned := len(warnings) > 0; warned != tc.warns {
				t.Errorf("got warnings %v, want warned=%v", warningLines(warnings), tc.warns)
			}
		})
	}
}

func TestMapAssignees(t *testing.T) {
	m := DefaultFieldMap()
	t.Run("a single user maps to its login", func(t *testing.T) {
		got, warnings := mapAssignees(issueWithField(m.AssigneeField, `{"login":"ana","fullName":"Ana Ruiz"}`), m)
		if len(got) != 1 || got[0] != "ana" || len(warnings) != 0 {
			t.Fatalf("got %v with warnings %v", got, warningLines(warnings))
		}
	})
	t.Run("a multi-user field maps every login and dedupes", func(t *testing.T) {
		got, _ := mapAssignees(issueWithField(m.AssigneeField, `[{"login":"ana"},{"login":"jose"},{"login":"ana"}]`), m)
		if len(got) != 2 || got[0] != "ana" || got[1] != "jose" {
			t.Fatalf("got %v", got)
		}
	})
	t.Run("a user without a login warns", func(t *testing.T) {
		got, warnings := mapAssignees(issueWithField(m.AssigneeField, `{"fullName":"Ana Ruiz"}`), m)
		if len(got) != 1 || got[0] != "Ana Ruiz" {
			t.Fatalf("got %v", got)
		}
		if len(warnings) != 1 {
			t.Fatalf("got warnings %v, want one", warningLines(warnings))
		}
	})
	t.Run("an unassigned issue has no assignees", func(t *testing.T) {
		got, warnings := mapAssignees(issueWithField(m.AssigneeField, `null`), m)
		if got != nil || len(warnings) != 0 {
			t.Fatalf("got %v with warnings %v", got, warningLines(warnings))
		}
	})
}

func TestMapLabels(t *testing.T) {
	issue := youtrack.Issue{Tags: []youtrack.Tag{{Name: "security"}, {Name: " "}, {Name: "security"}, {Name: "media"}}}
	got := mapLabels(issue)
	if len(got) != 2 || got[0] != "security" || got[1] != "media" {
		t.Fatalf("got %v, want [security media]", got)
	}
	if mapLabels(youtrack.Issue{}) != nil {
		t.Error("an untagged issue has no labels")
	}
}

func TestMapMilestone(t *testing.T) {
	m := DefaultFieldMap()
	t.Run("one version becomes the milestone", func(t *testing.T) {
		got, warnings := mapMilestone(issueWithField(m.MilestoneField, `[{"name":"1.5.0","$type":"VersionBundleElement"}]`), m)
		if got != "1.5.0" || len(warnings) != 0 {
			t.Fatalf("got %q with warnings %v", got, warningLines(warnings))
		}
	})
	t.Run("several versions keep the first and warn", func(t *testing.T) {
		got, warnings := mapMilestone(issueWithField(m.MilestoneField, `[{"name":"1.5.0"},{"name":"1.4.3"}]`), m)
		if got != "1.5.0" || len(warnings) != 1 {
			t.Fatalf("got %q with warnings %v", got, warningLines(warnings))
		}
	})
}

// TestIssueToDraftExternal checks the external reference every draft and patch
// carries, including the shape a missing base URL produces.
func TestIssueToDraftExternal(t *testing.T) {
	issue := youtrack.Issue{IDReadable: "ACME-42", Summary: "Login fails on Safari"}
	cases := []struct {
		name    string
		baseURL string
		wantURL string
	}{
		{name: "a plain host", baseURL: "https://yt.example.com", wantURL: "https://yt.example.com/issue/ACME-42"},
		{name: "a context path is kept", baseURL: "https://yt.example.com/youtrack", wantURL: "https://yt.example.com/youtrack/issue/ACME-42"},
		{name: "a trailing slash is trimmed", baseURL: "https://yt.example.com/youtrack/", wantURL: "https://yt.example.com/youtrack/issue/ACME-42"},
		{name: "no base url means no url", baseURL: "", wantURL: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			draft, _, _ := IssueToDraft(issue, Options{BaseURL: tc.baseURL})
			if len(draft.External) != 1 {
				t.Fatalf("got %d external references, want 1", len(draft.External))
			}
			got := draft.External[0]
			if got.System != System || got.ID != "ACME-42" || got.URL != tc.wantURL {
				t.Errorf("got %+v, want url %q", got, tc.wantURL)
			}
			patch, _, _ := IssueToPatch(issue, Options{BaseURL: tc.baseURL})
			if len(patch.AddExternal) != 1 || patch.AddExternal[0] != got {
				t.Errorf("the patch external %+v differs from the draft one %+v", patch.AddExternal, got)
			}
			if patch.External != nil {
				t.Error("a patch must add its external reference, not replace the list")
			}
		})
	}
}

// TestIssueToDraftLeavesRelationsOffTheDraft pins the seam: a YouTrack id must
// never reach a parent, milestone or links field as if it were an item id.
func TestIssueToDraftLeavesRelationsOffTheDraft(t *testing.T) {
	issue := loadIssue(t, "story.json")
	draft, relations, _ := IssueToDraft(issue, testOptions("ACME-US-0042"))
	if draft.Parent != "" || draft.Milestone != "" || draft.Links != nil {
		t.Errorf("the draft carries unresolved relations: parent=%q milestone=%q links=%v", draft.Parent, draft.Milestone, draft.Links)
	}
	if relations.Parent != "ACME-30" {
		t.Errorf("got parent %q, want ACME-30", relations.Parent)
	}
	patch, _, _ := IssueToPatch(issue, testOptions("ACME-US-0042"))
	if patch.Parent != nil || patch.Milestone != nil || patch.Links != nil || patch.AddLinks != nil {
		t.Error("the patch carries unresolved relations")
	}
}

// TestIssueToPatchIsSparse checks that an issue carrying nothing leaves every
// git-in-track-owned field alone.
func TestIssueToPatchIsSparse(t *testing.T) {
	patch, _, _ := IssueToPatch(youtrack.Issue{IDReadable: "ACME-1"}, Options{})
	if patch.Title != nil || patch.Status != nil || patch.Assignees != nil || patch.Labels != nil ||
		patch.Estimate != nil || patch.Body != nil {
		t.Errorf("got a non-sparse patch: %+v", patch)
	}
	if patch.Priority == nil || *patch.Priority != core.PriorityMedium {
		t.Error("an issue with no priority field still takes the mapped default")
	}
}

// TestIssueToDraftEmptySummaryWarns keeps a titleless issue reportable.
func TestIssueToDraftEmptySummaryWarns(t *testing.T) {
	_, _, warnings := IssueToDraft(youtrack.Issue{IDReadable: "ACME-1"}, Options{})
	found := false
	for _, w := range warnings {
		if w.Field == "summary" {
			found = true
		}
	}
	if !found {
		t.Errorf("got warnings %v, want one about the summary", warningLines(warnings))
	}
}

func points(v float64) *float64 { return &v }
