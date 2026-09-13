package mapping

import (
	"testing"

	"github.com/digiogithub/git-in-track/internal/core"
)

func TestFieldMapWithDefaults(t *testing.T) {
	t.Run("a zero field map is the default one", func(t *testing.T) {
		got := FieldMap{}.withDefaults()
		want := DefaultFieldMap()
		if got.TypeField != want.TypeField || got.StateField != want.StateField ||
			got.PriorityField != want.PriorityField || got.EstimationField != want.EstimationField ||
			got.AssigneeField != want.AssigneeField || got.MilestoneField != want.MilestoneField {
			t.Errorf("got field names %+v", got)
		}
		if got.MinutesPerPoint != defaultMinutesPerPoint || got.MinutesPerDay != defaultMinutesPerDay ||
			got.MinutesPerWeek != defaultMinutesPerWeek {
			t.Errorf("got period arithmetic %v/%v/%v", got.MinutesPerPoint, got.MinutesPerDay, got.MinutesPerWeek)
		}
	})
	t.Run("one override keeps every other default", func(t *testing.T) {
		got := FieldMap{StateField: "  Estado  "}.withDefaults()
		if got.StateField != "Estado" {
			t.Errorf("got state field %q", got.StateField)
		}
		if got.TypeField != "Type" {
			t.Errorf("got type field %q, want the default", got.TypeField)
		}
		if len(got.Statuses) == 0 {
			t.Error("the default status map must survive a field-name override")
		}
	})
	t.Run("map keys are normalised however they were typed", func(t *testing.T) {
		got := FieldMap{Types: map[string]core.ItemType{"  SPIKE ": core.TypeStory}}.withDefaults()
		if got.Types["spike"] != core.TypeStory {
			t.Errorf("got %+v", got.Types)
		}
	})
	t.Run("a supplied value map replaces the defaults rather than merging", func(t *testing.T) {
		got := FieldMap{Statuses: map[string]core.Status{"parked": core.Status("backlog")}}.withDefaults()
		if _, found := got.Statuses["fixed"]; found {
			t.Error("a project that declares its own states means exactly those states")
		}
	})
	t.Run("DefaultFieldMap hands out a fresh copy", func(t *testing.T) {
		first := DefaultFieldMap()
		first.Types["spike"] = core.TypeStory
		if _, found := DefaultFieldMap().Types["spike"]; found {
			t.Error("mutating one copy leaked into the next")
		}
	})
	t.Run("the default status is empty so the project default applies", func(t *testing.T) {
		if DefaultFieldMap().DefaultStatus != "" {
			t.Error("the default field map must not pin a status id it cannot know")
		}
	})
	t.Run("every default maps to a value core accepts", func(t *testing.T) {
		d := DefaultFieldMap()
		for value, mapped := range d.Types {
			if !mapped.Valid() {
				t.Errorf("the type %q maps to the invalid item type %q", value, mapped)
			}
		}
		for value, mapped := range d.Priorities {
			if !mapped.Valid() {
				t.Errorf("the priority %q maps to the invalid priority %q", value, mapped)
			}
		}
	})
}

func TestWarningString(t *testing.T) {
	cases := []struct {
		name string
		in   Warning
		want string
	}{
		{name: "with a value", in: Warning{Field: "State", Value: "Parked", Reason: "unknown"}, want: "State: Parked: unknown"},
		{name: "without a value", in: Warning{Field: "summary", Reason: "empty"}, want: "summary: empty"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.in.String(); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSortWarningsIsDeterministic(t *testing.T) {
	got := sortWarnings([]Warning{
		{Field: "links", Value: "b"},
		{Field: "State", Value: "a"},
		{Field: "links", Value: "a"},
	})
	if len(got) != 3 || got[0].Field != "State" || got[1].Value != "a" || got[2].Value != "b" {
		t.Errorf("got %+v", got)
	}
	if sortWarnings(nil) != nil {
		t.Error("no warnings must stay nil")
	}
}

func TestFieldMapWithFieldNames(t *testing.T) {
	got := DefaultFieldMap().WithFieldNames(map[string]string{
		KeyStatus:    "Estado",
		KeyType:      "  Tipo  ",
		KeyPriority:  "",
		"labels":     "Etiquetas",
		"unknownkey": "Nope",
	}).withDefaults()

	if got.StateField != "Estado" {
		t.Errorf("got state field %q, want Estado", got.StateField)
	}
	if got.TypeField != "Tipo" {
		t.Errorf("got type field %q, want Tipo (trimmed)", got.TypeField)
	}
	if got.PriorityField != "Priority" {
		t.Errorf("an empty entry must leave the name alone, got %q", got.PriorityField)
	}
	if got.AssigneeField != "Assignee" || got.EstimationField != "Estimation" || got.MilestoneField != "Fix versions" {
		t.Errorf("an unmentioned field must keep its default, got %+v", got)
	}
	if len(got.Statuses) == 0 {
		t.Error("the value maps must survive a field-name override")
	}
}
