package mapping

import (
	"encoding/json"
	"testing"

	"github.com/digiogithub/git-in-track/internal/youtrack"
)

// TestReduceFieldValue walks the $type table of the YouTrack API reference,
// section 3.7: one case per field kind the mapper can meet.
func TestReduceFieldValue(t *testing.T) {
	cases := []struct {
		name  string
		raw   string
		want  string
		found bool
	}{
		{name: "single enum reduces on name", raw: `{"id":"70-1","name":"Bug","$type":"EnumBundleElement"}`, want: "Bug", found: true},
		{name: "state reduces on name", raw: `{"id":"71-3","name":"In Progress","isResolved":false,"$type":"StateBundleElement"}`, want: "In Progress", found: true},
		{name: "user reduces on login before full name", raw: `{"id":"1-7","login":"ana","fullName":"Ana Ruiz","$type":"User"}`, want: "ana", found: true},
		{name: "user without a login falls back to the full name", raw: `{"id":"1-9","fullName":"Ana Ruiz","$type":"User"}`, want: "Ana Ruiz", found: true},
		{name: "localized name comes before presentation", raw: `{"id":"9","localizedName":"Abierto","presentation":"Open","$type":"StateBundleElement"}`, want: "Abierto", found: true},
		{name: "period reduces on presentation", raw: `{"id":"0","presentation":"3d 4h","$type":"PeriodValue"}`, want: "3d 4h", found: true},
		{name: "period with minutes only renders the minutes", raw: `{"id":"0","minutes":90,"$type":"PeriodValue"}`, want: "0", found: true},
		{name: "version reduces on name", raw: `{"id":"113-1","name":"1.5.0","$type":"VersionBundleElement"}`, want: "1.5.0", found: true},
		{name: "issue reference reduces on idReadable", raw: `{"id":"2-1","idReadable":"ACME-1","$type":"Issue"}`, want: "ACME-1", found: true},
		{name: "last resort is the internal id", raw: `{"id":"70-9","$type":"EnumBundleElement"}`, want: "70-9", found: true},
		{name: "an object with nothing usable is not reduced", raw: `{"$type":"EnumBundleElement"}`, want: "", found: false},
		{name: "a text scalar loses its quotes", raw: `"free text"`, want: "free text", found: true},
		{name: "an integer scalar passes through", raw: `13`, want: "13", found: true},
		{name: "a float scalar passes through", raw: `1.5`, want: "1.5", found: true},
		{name: "a boolean scalar passes through", raw: `true`, want: "true", found: true},
		{name: "a date scalar passes through as unix milliseconds", raw: `1768435200000`, want: "1768435200000", found: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			values := decodeValues(t, tc.raw)
			if len(values) != 1 {
				t.Fatalf("got %d decoded values, want 1", len(values))
			}
			// A period sent as minutes only has an id of "0", which the
			// documented order reaches before the minutes fallback.
			got, ok := reduceFieldValue(values[0])
			if ok != tc.found {
				t.Fatalf("got reduced=%v, want %v", ok, tc.found)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// TestReduceFieldValueMinutesOnly covers the period value that carries nothing
// but its minutes, which no key of the documented order can reach.
func TestReduceFieldValueMinutesOnly(t *testing.T) {
	values := decodeValues(t, `{"minutes":90,"$type":"PeriodValue"}`)
	got, ok := reduceFieldValue(values[0])
	if !ok || got != "90m" {
		t.Fatalf("got (%q, %v), want (\"90m\", true)", got, ok)
	}
}

// TestFieldValuesMulti covers a multi-value field, which YouTrack sends as an
// array and the reducer maps element-wise.
func TestFieldValuesMulti(t *testing.T) {
	issue := issueWithField("Sprints", `[{"id":"112-9","name":"Sprint 12"},{"id":"112-10","name":"Sprint 13"}]`)
	values, warnings := fieldValues(issue, "Sprints")
	if len(warnings) != 0 {
		t.Fatalf("got warnings %v, want none", warningLines(warnings))
	}
	if len(values) != 2 || values[0].Text != "Sprint 12" || values[1].Text != "Sprint 13" {
		t.Fatalf("got %+v", values)
	}
}

// TestFieldValuesUnreducable reports an object whose every known key is empty
// instead of dropping it.
func TestFieldValuesUnreducable(t *testing.T) {
	issue := issueWithField("Type", `{"$type":"EnumBundleElement"}`)
	values, warnings := fieldValues(issue, "Type")
	if len(values) != 0 {
		t.Fatalf("got %d values, want none", len(values))
	}
	if len(warnings) != 1 || warnings[0].Field != "Type" {
		t.Fatalf("got warnings %v, want one about Type", warningLines(warnings))
	}
}

// TestFieldValuesAbsentOrNull is silent: "not set" is a normal YouTrack state,
// not a mapping failure.
func TestFieldValuesAbsentOrNull(t *testing.T) {
	for name, issue := range map[string]youtrack.Issue{
		"absent": {},
		"null":   issueWithField("State", `null`),
	} {
		t.Run(name, func(t *testing.T) {
			values, warnings := fieldValues(issue, "State")
			if len(values) != 0 || len(warnings) != 0 {
				t.Fatalf("got %d values and %d warnings, want none", len(values), len(warnings))
			}
		})
	}
}

func TestIsVersionValue(t *testing.T) {
	if !isVersionValue(Value{Type: "VersionBundleElement"}) {
		t.Error("a VersionBundleElement is a version value")
	}
	if isVersionValue(Value{Type: "EnumBundleElement"}) {
		t.Error("an EnumBundleElement is not a version value")
	}
}

// decodeValues decodes a raw custom-field value the way the client does.
func decodeValues(t *testing.T, raw string) []youtrack.FieldValue {
	t.Helper()
	var v youtrack.CustomFieldValue
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		t.Fatalf("decoding %s: %v", raw, err)
	}
	values, err := v.Values()
	if err != nil {
		t.Fatalf("reducing %s: %v", raw, err)
	}
	return values
}

// issueWithField builds an issue carrying exactly one custom field.
func issueWithField(name, raw string) youtrack.Issue {
	var v youtrack.CustomFieldValue
	_ = json.Unmarshal([]byte(raw), &v)
	return youtrack.Issue{CustomFields: []youtrack.CustomField{{Name: name, Value: v}}}
}
