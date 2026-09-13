package mapping

import (
	"strings"

	"github.com/digiogithub/git-in-track/internal/core"
)

// FieldMap is the translation table between one YouTrack project and one
// git-in-track project. It is data on purpose: a project that renamed "State"
// to "Estado" or added a "Spike" issue type configures this struct and changes
// no code. Every entry is optional; withDefaults fills what the caller left
// unset from DefaultFieldMap, so a caller may override a single value map
// without restating the field names.
//
// The value maps are looked up case-insensitively on the trimmed YouTrack
// value, so "In Progress", "in progress" and " IN PROGRESS " are one key.
type FieldMap struct {
	// The names of the YouTrack custom fields to read.
	TypeField       string
	StateField      string
	PriorityField   string
	EstimationField string
	AssigneeField   string
	MilestoneField  string

	// Types maps a YouTrack Type value onto a git-in-track item type.
	Types map[string]core.ItemType
	// Statuses maps a YouTrack State value onto a project.yaml status id.
	Statuses map[string]core.Status
	// Priorities maps a YouTrack Priority value onto one of the four core
	// priorities.
	Priorities map[string]core.Priority

	// The fallbacks used, with a warning, when a value is not in a map. An
	// empty DefaultStatus leaves the status unset so that the project default
	// applies (R-DEFAULT).
	DefaultType     core.ItemType
	DefaultStatus   core.Status
	DefaultPriority core.Priority

	// MinutesPerPoint converts a YouTrack Estimation period into story points.
	// The default is one point per working day.
	MinutesPerPoint float64
	// MinutesPerDay and MinutesPerWeek are how the "d" and "w" units of a
	// period presentation such as "1w 2d 3h" are read. YouTrack's stock
	// working week is five eight-hour days.
	MinutesPerDay  float64
	MinutesPerWeek float64
}

// Default period arithmetic, matching a stock YouTrack working week.
const (
	defaultMinutesPerDay   = 8 * 60
	defaultMinutesPerWeek  = 5 * defaultMinutesPerDay
	defaultMinutesPerPoint = defaultMinutesPerDay
)

// DefaultFieldMap returns the mapping that works against a stock YouTrack:
// the default field names, the default enum bundles ("Submitted", "Open",
// "In Progress", "To be discussed", "Reopened", "Can't Reproduce", "Duplicate",
// "Fixed", "Won't fix", "Incomplete", "Obsolete", "Verified") and the default
// priority bundle ("Show-stopper", "Critical", "Major", "Normal", "Minor").
//
// Callers get a fresh copy every time, so mutating the result is safe.
func DefaultFieldMap() FieldMap {
	return FieldMap{
		TypeField:       "Type",
		StateField:      "State",
		PriorityField:   "Priority",
		EstimationField: "Estimation",
		AssigneeField:   "Assignee",
		MilestoneField:  "Fix versions",

		Types: map[string]core.ItemType{
			"epic":                core.TypeEpic,
			"user story":          core.TypeStory,
			"story":               core.TypeStory,
			"feature":             core.TypeStory,
			"task":                core.TypeTask,
			"bug":                 core.TypeTask,
			"usability problem":   core.TypeTask,
			"performance problem": core.TypeTask,
			"cosmetics":           core.TypeTask,
			"exception":           core.TypeTask,
			"milestone":           core.TypeMilestone,
			"version":             core.TypeMilestone,
		},
		Statuses: map[string]core.Status{
			"submitted":        core.Status("backlog"),
			"to be discussed":  core.Status("backlog"),
			"open":             core.Status("todo"),
			"reopened":         core.Status("todo"),
			"in progress":      core.Status("in_progress"),
			"to verify":        core.Status("in_review"),
			"in review":        core.Status("in_review"),
			"fixed":            core.Status("done"),
			"verified":         core.Status("done"),
			"done":             core.Status("done"),
			"can't reproduce":  core.Status("cancelled"),
			"cannot reproduce": core.Status("cancelled"),
			"duplicate":        core.Status("cancelled"),
			"won't fix":        core.Status("cancelled"),
			"obsolete":         core.Status("cancelled"),
			"incomplete":       core.Status("cancelled"),
		},
		Priorities: map[string]core.Priority{
			"show-stopper": core.PriorityCritical,
			"showstopper":  core.PriorityCritical,
			"critical":     core.PriorityCritical,
			"blocker":      core.PriorityCritical,
			"major":        core.PriorityHigh,
			"high":         core.PriorityHigh,
			"normal":       core.PriorityMedium,
			"medium":       core.PriorityMedium,
			"minor":        core.PriorityLow,
			"low":          core.PriorityLow,
		},

		DefaultType:     core.TypeTask,
		DefaultPriority: core.PriorityMedium,

		MinutesPerPoint: defaultMinutesPerPoint,
		MinutesPerDay:   defaultMinutesPerDay,
		MinutesPerWeek:  defaultMinutesPerWeek,
	}
}

// withDefaults returns a copy of m with every unset entry filled in from
// DefaultFieldMap. A value map the caller supplied is used as given and is not
// merged with the default one: a project that declares its own states means
// exactly those states, and silently re-adding "Fixed" behind its back would
// be a surprise. The map keys are normalised, so a caller may write them in
// whatever case the YouTrack UI shows.
func (m FieldMap) withDefaults() FieldMap {
	d := DefaultFieldMap()
	out := m
	for _, pair := range []struct {
		dst *string
		def string
	}{
		{&out.TypeField, d.TypeField},
		{&out.StateField, d.StateField},
		{&out.PriorityField, d.PriorityField},
		{&out.EstimationField, d.EstimationField},
		{&out.AssigneeField, d.AssigneeField},
		{&out.MilestoneField, d.MilestoneField},
	} {
		if strings.TrimSpace(*pair.dst) == "" {
			*pair.dst = pair.def
		} else {
			*pair.dst = strings.TrimSpace(*pair.dst)
		}
	}
	if out.Types == nil {
		out.Types = d.Types
	}
	if out.Statuses == nil {
		out.Statuses = d.Statuses
	}
	if out.Priorities == nil {
		out.Priorities = d.Priorities
	}
	out.Types = normalizeKeys(out.Types)
	out.Statuses = normalizeKeys(out.Statuses)
	out.Priorities = normalizeKeys(out.Priorities)
	if out.DefaultType == "" {
		out.DefaultType = d.DefaultType
	}
	if out.DefaultPriority == "" {
		out.DefaultPriority = d.DefaultPriority
	}
	if out.MinutesPerPoint <= 0 {
		out.MinutesPerPoint = d.MinutesPerPoint
	}
	if out.MinutesPerDay <= 0 {
		out.MinutesPerDay = d.MinutesPerDay
	}
	if out.MinutesPerWeek <= 0 {
		out.MinutesPerWeek = d.MinutesPerWeek
	}
	return out
}

// normalizeKeys lower-cases and trims every key of a value map, so that a
// lookup on a YouTrack value never depends on how the bundle was typed.
func normalizeKeys[T ~string](in map[string]T) map[string]T {
	out := make(map[string]T, len(in))
	for k, v := range in {
		out[normalizeKey(k)] = v
	}
	return out
}

// normalizeKey is the single normalisation applied to both the map keys and
// the incoming YouTrack values: trimmed and folded to lower case.
func normalizeKey(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// Field-map keys, matching the `integrations.youtrack.field_map` block of a
// project file: each names a git-in-track field and carries the name of the
// YouTrack custom field holding it.
const (
	KeyStatus    = "status"
	KeyPriority  = "priority"
	KeyType      = "type"
	KeyAssignee  = "assignee"
	KeyEstimate  = "estimate"
	KeyMilestone = "milestone"
)

// WithFieldNames returns a copy of m with the YouTrack custom-field names taken
// from a "git-in-track field → YouTrack field name" mapping, which is the shape
// the `integrations.youtrack.field_map` block of a project file has.
//
// It is the adapter between the configuration and this package, and it is
// written the other way round on purpose: the config package holds credentials
// and paths and must not depend on the mapping rules, so the caller that has
// both hands the strings over. Keys this package does not consume — "labels",
// "due", "sprint" — are ignored, because tags already carry the labels and the
// other two are not mapped. An empty value leaves the current name alone.
func (m FieldMap) WithFieldNames(names map[string]string) FieldMap {
	out := m
	for key, target := range map[string]*string{
		KeyStatus:    &out.StateField,
		KeyPriority:  &out.PriorityField,
		KeyType:      &out.TypeField,
		KeyAssignee:  &out.AssigneeField,
		KeyEstimate:  &out.EstimationField,
		KeyMilestone: &out.MilestoneField,
	} {
		if name := strings.TrimSpace(names[key]); name != "" {
			*target = name
		}
	}
	return out
}
