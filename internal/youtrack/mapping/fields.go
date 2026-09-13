package mapping

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/digiogithub/git-in-track/internal/youtrack"
)

// Value is one custom-field value reduced to the string a human sees, with the
// raw "$type" discriminator kept beside it so a caller can still tell a period
// from an enum and a version from a state. The reference CLI throws the type
// away at this point, which is exactly what makes it lossy (see the YouTrack
// API reference, section 3.7).
type Value struct {
	// Text is the reduced presentation of the value, empty when nothing in it
	// could be reduced.
	Text string
	// Type is the raw "$type" of the value, for example "PeriodValue" or
	// "VersionValue". It is empty for a scalar.
	Type string
	// Raw is the decoded value, kept so that a mapper needing Minutes,
	// IsResolved or the internal id does not have to decode twice.
	Raw youtrack.FieldValue
}

// IsZero reports whether nothing was reduced and nothing was typed.
func (v Value) IsZero() bool { return v.Text == "" && v.Type == "" }

// reduceFieldValue reduces one decoded custom-field value to its display
// string, following the order the reference implementation documents:
//
//	name → login → fullName → localizedName → presentation → idReadable → id
//
// A scalar (a text, integer, float or date field, which YouTrack sends as a
// bare JSON value rather than an object) passes through as its literal text.
// The boolean reports whether anything at all could be reduced; false means an
// object whose every known key was empty, which is a value shape this package
// does not understand and the caller turns into a Warning.
func reduceFieldValue(v youtrack.FieldValue) (string, bool) {
	for _, candidate := range []string{
		v.Name,
		v.Login,
		v.FullName,
		v.LocalizedName,
		v.Presentation,
		v.IDReadable,
		v.ID,
	} {
		if trimmed := strings.TrimSpace(candidate); trimmed != "" {
			return trimmed, true
		}
	}
	if s, ok := scalarText(v.Scalar); ok {
		return s, true
	}
	// A period field sent as minutes only has no presentation at all.
	if v.Minutes != nil {
		return strconv.Itoa(*v.Minutes) + "m", true
	}
	return "", false
}

// scalarText renders the raw JSON of a non-object custom-field value. A JSON
// string loses its quotes; a number, a boolean and anything else keep their
// literal form. JSON null reduces to nothing.
func scalarText(raw json.RawMessage) (string, bool) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return "", false
	}
	if trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal([]byte(trimmed), &s); err == nil {
			s = strings.TrimSpace(s)
			return s, s != ""
		}
	}
	return trimmed, true
}

// fieldValues reduces every value of one named custom field. A field that is
// absent or null yields no values and no warning: "not set" is a normal state
// in YouTrack and is not a mapping failure. A value whose shape could not be
// reduced yields a warning and is skipped.
func fieldValues(issue youtrack.Issue, name string) ([]Value, []Warning) {
	field, ok := issue.CustomField(name)
	if !ok || field.Value.IsNull() {
		return nil, nil
	}
	decoded, err := field.Value.Values()
	if err != nil {
		return nil, []Warning{{
			Field:  name,
			Reason: fmt.Sprintf("the value of the %q field could not be decoded: %v", name, err),
		}}
	}
	var (
		out      []Value
		warnings []Warning
	)
	for _, raw := range decoded {
		text, reduced := reduceFieldValue(raw)
		if !reduced {
			warnings = append(warnings, Warning{
				Field:  name,
				Reason: fmt.Sprintf("a value of the %q field has no name, login, presentation or id and was skipped", name),
			})
			continue
		}
		out = append(out, Value{Text: text, Type: raw.Type, Raw: raw})
	}
	return out, warnings
}

// firstFieldValue reduces the first value of a named custom field, which is
// what a single-value field such as State, Priority or Assignee always has.
func firstFieldValue(issue youtrack.Issue, name string) (Value, bool, []Warning) {
	values, warnings := fieldValues(issue, name)
	if len(values) == 0 {
		return Value{}, false, warnings
	}
	return values[0], true, warnings
}

// isVersionValue reports whether a value came out of a version bundle, which is
// the closest YouTrack analog of a git-in-track milestone. The check is on
// the raw "$type" rather than on the field name, so it holds whatever the
// project called the field.
func isVersionValue(v Value) bool {
	return strings.Contains(v.Type, "Version")
}
