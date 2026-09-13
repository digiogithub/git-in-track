package config

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// The `integrations.youtrack.field_map` block, tasks GIT-T-0131 and
// GIT-T-0136.
//
// A field map answers two questions, and until now it could only answer the
// first:
//
//  1. *Which* YouTrack custom field carries a git-in-track field — the field
//     called "State" carries `status`.
//  2. *What* one of that field's values means here — the value "In Progress"
//     is the local status `in_progress`.
//
// The second is what a per-value mapping adds. It is expressed by nesting, and
// the flat spelling keeps its old meaning exactly, so a project.yaml written
// before this build still loads and still means what it said:
//
//	field_map:
//	  priority: Priority             # the flat form: a field name, no value map
//	  status:
//	    field: State                 # the same thing, said the long way
//	    values:
//	      In Progress: in_progress   # and what its values mean here
//	      Fixed: done
//
// The two forms are one type. A mapping with no values is written back as a
// scalar, so turning a value map on and then off again leaves the file as
// readable as it was found.

// FieldMapping is one entry of the block: the YouTrack custom field that
// carries a git-in-track field, and how that field's values translate.
type FieldMapping struct {
	// Field is the YouTrack custom field name, the "State" of a stock
	// instance. It is the name rather than the id because an id is
	// instance-local and a name is what a person reads in the UI.
	Field string `json:"field" yaml:"field"`
	// Values maps a YouTrack value name onto the git-in-track value it means:
	// a status id for `status`, one of the four core priorities for
	// `priority`, an item type for `type`. Keys are the value's `name`, not its
	// localized name, which changes with the UI language, and not its id, which
	// is instance-local.
	//
	// An empty map means "no per-value mapping": the importer's defaults apply.
	Values map[string]string `json:"values,omitempty" yaml:"values,omitempty"`
}

// FieldMap is the whole block, keyed by git-in-track field name. Keys are drawn
// from FieldMapKeys.
type FieldMap map[string]FieldMapping

// FieldMapValueKeys are the git-in-track fields whose *values* can be mapped.
// They are exactly the three the importer translates value by value: an
// assignee is resolved against the project's people, a milestone against its
// milestones, and an estimate is arithmetic. A `values` block on any other key
// would be silently ignored, so it is refused instead.
var FieldMapValueKeys = []string{"status", "priority", "type"}

// ValueMappable reports whether the values of a git-in-track field can be
// mapped one by one.
func ValueMappable(key string) bool {
	for _, known := range FieldMapValueKeys {
		if key == known {
			return true
		}
	}
	return false
}

// Names is the flat projection of the block: git-in-track field onto YouTrack
// field name, with every value map dropped.
//
// It is the shape internal/vault and internal/youtrack/mapping take, and it is
// deliberately lossy: a caller that wants the value maps asks for them by name
// with ValuesFor.
func (m FieldMap) Names() map[string]string {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]string, len(m))
	for key, entry := range m {
		if name := strings.TrimSpace(entry.Field); name != "" {
			out[key] = name
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// ValuesFor returns the value map of one git-in-track field, nil when it
// declares none.
func (m FieldMap) ValuesFor(key string) map[string]string {
	entry, ok := m[key]
	if !ok || len(entry.Values) == 0 {
		return nil
	}
	out := make(map[string]string, len(entry.Values))
	for from, to := range entry.Values {
		out[from] = to
	}
	return out
}

// Normalized returns the block with every key and value trimmed and every empty
// entry dropped, so that what is written back is what would be read.
func (m FieldMap) Normalized() FieldMap {
	if len(m) == 0 {
		return nil
	}
	out := make(FieldMap, len(m))
	for key, entry := range m {
		key = strings.TrimSpace(key)
		normalized := FieldMapping{Field: strings.TrimSpace(entry.Field)}
		for from, to := range entry.Values {
			from, to = strings.TrimSpace(from), strings.TrimSpace(to)
			if from == "" || to == "" {
				continue
			}
			if normalized.Values == nil {
				normalized.Values = map[string]string{}
			}
			normalized.Values[from] = to
		}
		// A key with neither a field name nor a value map says nothing; a key
		// with only a value map is kept, because "the field is called what it
		// is called by default, and these are its values" is a real thing to
		// want to say.
		if key == "" || (normalized.Field == "" && len(normalized.Values) == 0) {
			continue
		}
		out[key] = normalized
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// Keys returns the git-in-track fields the block names, sorted, so that two
// runs report the same problems and write the same file.
func (m FieldMap) Keys() []string {
	out := make([]string, 0, len(m))
	for key := range m {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

// validate reports every problem in the block at once, naming the exact key
// each one is about so a form can mark the input that was wrong.
func (m FieldMap) validate(add func(field, format string, args ...any)) {
	for _, key := range m.Keys() {
		entry := m[key]
		if reason, retired := retiredFieldMapKeys[key]; retired {
			add("field_map."+key,
				"%q is no longer mapped and never was honored: %s. Remove the entry.", key, reason)
			continue
		}
		if !knownFieldMapKey(key) {
			add("field_map."+key, "%q is not a git-in-track field: use one of %s",
				key, strings.Join(FieldMapKeys, ", "))
			continue
		}
		if len(entry.Values) > 0 && !ValueMappable(key) {
			add("field_map."+key+".values",
				"the values of %q cannot be mapped one by one: only %s carry a value map",
				key, strings.Join(FieldMapValueKeys, ", "))
		}
		for _, from := range sortedKeys(entry.Values) {
			if strings.TrimSpace(entry.Values[from]) == "" {
				add("field_map."+key+".values."+from,
					"maps to nothing: give the git-in-track value %q means, or drop the entry", from)
			}
		}
	}
}

// UnmarshalYAML accepts both spellings of an entry: the scalar field name of
// the flat form and the mapping of the nested one.
func (e *FieldMapping) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
		e.Field, e.Values = node.Value, nil
		return nil
	case yaml.MappingNode:
		// A named alias type, so decoding the mapping does not call this method
		// again and recurse forever.
		type plain FieldMapping
		var decoded plain
		if err := node.Decode(&decoded); err != nil {
			return fmt.Errorf("field_map entry: %w", err)
		}
		*e = FieldMapping(decoded)
		return nil
	default:
		return fmt.Errorf("a field_map entry is either a YouTrack field name or a {field, values} mapping, not %s",
			yamlKindName(node.Kind))
	}
}

// MarshalYAML writes the flat form when there is nothing a value map would add,
// which is what keeps a hand-written project.yaml readable.
func (e FieldMapping) MarshalYAML() (any, error) {
	if len(e.Values) == 0 {
		return e.Field, nil
	}
	type plain FieldMapping
	return plain(e), nil
}

// UnmarshalJSON accepts the same two spellings over the wire, so a client that
// still sends `{"status":"State"}` keeps working.
func (e *FieldMapping) UnmarshalJSON(data []byte) error {
	var name string
	if err := json.Unmarshal(data, &name); err == nil {
		e.Field, e.Values = name, nil
		return nil
	}
	type plain FieldMapping
	var decoded plain
	if err := json.Unmarshal(data, &decoded); err != nil {
		return fmt.Errorf("a field_map entry is either a YouTrack field name or a {field, values} object: %w", err)
	}
	*e = FieldMapping(decoded)
	return nil
}

// yamlKindName names a YAML node kind for an error message.
func yamlKindName(kind yaml.Kind) string {
	switch kind {
	case yaml.SequenceNode:
		return "a list"
	case yaml.AliasNode:
		return "an alias"
	case yaml.DocumentNode:
		return "a document"
	case yaml.MappingNode:
		return "a mapping"
	case yaml.ScalarNode:
		return "a scalar"
	default:
		return "that"
	}
}
