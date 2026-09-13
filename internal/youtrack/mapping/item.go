package mapping

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/digiogithub/git-in-track/internal/core"
	"github.com/digiogithub/git-in-track/internal/youtrack"
)

// IssueToDraft maps a YouTrack issue onto the draft of a new git-in-track item.
//
// The draft carries the title, the type, the status, the priority, the
// estimate, the assignees, the labels, the normalised body and the external
// reference. It deliberately does NOT carry `parent`, `milestone` or `links`:
// those name other items, this package cannot resolve a YouTrack id into an
// item id, and writing an unresolved id into one of those fields would produce
// a file that fails validation. They come back in Relations instead, and the
// importer resolves them through core.Index.ItemByExternal.
//
// Warnings report every value the field map did not understand, together with
// the fallback used. The result is always usable; there is no error return.
func IssueToDraft(issue youtrack.Issue, opts Options) (core.ItemDraft, Relations, []Warning) {
	m := opts.fieldMap()
	f := mapFields(issue, opts, m)

	draft := core.ItemDraft{
		Type:      f.itemType,
		Title:     strings.TrimSpace(issue.Summary),
		Status:    f.status,
		Priority:  f.priority,
		Assignees: f.assignees,
		Author:    userHandle(issue.Reporter),
		Labels:    f.labels,
		Estimate:  f.estimate,
		External:  []core.External{opts.External(issue.IDReadable, "issue")},
		Body:      f.body,
	}
	if draft.Title == "" {
		f.warnings = append(f.warnings, Warning{
			Field:  "summary",
			Reason: "the issue has an empty summary, so the item has no title",
		})
	}
	return draft, f.relations, sortWarnings(f.warnings)
}

// IssueToPatch maps a YouTrack issue onto a sparse patch for an item that
// already exists, which is what a re-import of a previously imported issue
// applies. Only the fields YouTrack actually carried are set, so a field
// git-in-track owns alone — sprint, owner, effort, due — is never cleared by a
// sync.
//
// The external reference is pushed through AddExternal rather than External so
// that a second tracker already recorded on the item survives (docs/07
// section 5.5), and `parent`, `milestone` and `links` are again left to the
// importer through Relations.
func IssueToPatch(issue youtrack.Issue, opts Options) (core.ItemPatch, Relations, []Warning) {
	m := opts.fieldMap()
	f := mapFields(issue, opts, m)

	patch := core.ItemPatch{
		AddExternal: []core.External{opts.External(issue.IDReadable, "issue")},
	}
	if title := strings.TrimSpace(issue.Summary); title != "" {
		patch.Title = &title
	}
	if f.status != "" {
		status := f.status
		patch.Status = &status
	}
	if f.priority != "" {
		priority := f.priority
		patch.Priority = &priority
	}
	if f.assignees != nil {
		assignees := f.assignees
		patch.Assignees = &assignees
	}
	if f.labels != nil {
		labels := f.labels
		patch.Labels = &labels
	}
	if f.estimate != nil {
		patch.Estimate = f.estimate
	}
	if f.body != "" {
		body := f.body
		patch.Body = &body
	}
	return patch, f.relations, sortWarnings(f.warnings)
}

// mappedFields is everything the per-field mappers produced for one issue.
type mappedFields struct {
	itemType  core.ItemType
	status    core.Status
	priority  core.Priority
	estimate  *float64
	assignees []string
	labels    []string
	body      string
	relations Relations
	warnings  []Warning
}

// mapFields runs every per-field mapper once, so that IssueToDraft and
// IssueToPatch cannot drift apart.
func mapFields(issue youtrack.Issue, opts Options, m FieldMap) mappedFields {
	var out mappedFields
	collect := func(ws []Warning) { out.warnings = append(out.warnings, ws...) }

	var ws []Warning
	out.itemType, ws = mapType(issue, m)
	collect(ws)
	out.status, ws = mapStatus(issue, m)
	collect(ws)
	out.priority, ws = mapPriority(issue, m)
	collect(ws)
	out.estimate, ws = mapEstimate(issue, m)
	collect(ws)
	out.assignees, ws = mapAssignees(issue, m)
	collect(ws)
	out.labels = mapLabels(issue)

	milestone, ws := mapMilestone(issue, m)
	collect(ws)

	body, ws := normalizeBody(issue.Description, opts.ItemID, opts.attachmentPrefix())
	collect(ws)
	out.body = body

	parent, children, links, ws := MapLinks(issue)
	collect(ws)
	out.relations = Relations{Parent: parent, Children: children, Milestone: milestone, Links: links}
	return out
}

// mapType maps the Type custom field onto a core item type. A value that came
// out of a version bundle is a milestone whatever it is called, because a
// version is the only YouTrack concept that stands in for one. An unknown
// value falls back to FieldMap.DefaultType and warns; an absent field falls
// back silently, since "not set" is not a mapping failure.
func mapType(issue youtrack.Issue, m FieldMap) (core.ItemType, []Warning) {
	value, ok, warnings := firstFieldValue(issue, m.TypeField)
	if !ok {
		return m.DefaultType, warnings
	}
	if isVersionValue(value) {
		return core.TypeMilestone, warnings
	}
	if mapped, found := m.Types[normalizeKey(value.Text)]; found {
		if !mapped.Valid() {
			return m.DefaultType, append(warnings, Warning{
				Field:    m.TypeField,
				Value:    value.Text,
				Fallback: string(m.DefaultType),
				Reason:   fmt.Sprintf("the field map turns %q into %q, which is not a git-in-track item type", value.Text, mapped),
			})
		}
		return mapped, warnings
	}
	return m.DefaultType, append(warnings, Warning{
		Field:    m.TypeField,
		Value:    value.Text,
		Fallback: string(m.DefaultType),
		Reason:   fmt.Sprintf("the issue type %q is not in the field map", value.Text),
	})
}

// mapStatus maps the State custom field onto a project.yaml status id. An
// unknown state falls back to FieldMap.DefaultStatus, which is empty by
// default so that the project's own default status applies (R-DEFAULT).
func mapStatus(issue youtrack.Issue, m FieldMap) (core.Status, []Warning) {
	value, ok, warnings := firstFieldValue(issue, m.StateField)
	if !ok {
		return m.DefaultStatus, warnings
	}
	if mapped, found := m.Statuses[normalizeKey(value.Text)]; found {
		return mapped, warnings
	}
	return m.DefaultStatus, append(warnings, Warning{
		Field:    m.StateField,
		Value:    value.Text,
		Fallback: string(m.DefaultStatus),
		Reason:   fmt.Sprintf("the state %q is not in the field map", value.Text),
	})
}

// mapPriority maps the Priority custom field onto one of the four core
// priorities.
func mapPriority(issue youtrack.Issue, m FieldMap) (core.Priority, []Warning) {
	value, ok, warnings := firstFieldValue(issue, m.PriorityField)
	if !ok {
		return m.DefaultPriority, warnings
	}
	if mapped, found := m.Priorities[normalizeKey(value.Text)]; found {
		if !mapped.Valid() {
			return m.DefaultPriority, append(warnings, Warning{
				Field:    m.PriorityField,
				Value:    value.Text,
				Fallback: string(m.DefaultPriority),
				Reason:   fmt.Sprintf("the field map turns %q into %q, which is not a git-in-track priority", value.Text, mapped),
			})
		}
		return mapped, warnings
	}
	return m.DefaultPriority, append(warnings, Warning{
		Field:    m.PriorityField,
		Value:    value.Text,
		Fallback: string(m.DefaultPriority),
		Reason:   fmt.Sprintf("the priority %q is not in the field map", value.Text),
	})
}

// mapAssignees maps the Assignee custom field onto the assignee handles. The
// field may be multi-valued on a project that uses a multi-user field, so every
// value is taken. The handle is the login, which is the stable identifier;
// a value carrying only a full name is used as-is and reported, because a full
// name is not a handle and will not match a project member.
func mapAssignees(issue youtrack.Issue, m FieldMap) ([]string, []Warning) {
	values, warnings := fieldValues(issue, m.AssigneeField)
	if len(values) == 0 {
		return nil, warnings
	}
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		login := strings.TrimSpace(value.Raw.Login)
		if login == "" {
			login = value.Text
			warnings = append(warnings, Warning{
				Field:    m.AssigneeField,
				Value:    value.Text,
				Fallback: login,
				Reason:   fmt.Sprintf("the assignee %q came back without a login, so its display name was used as the handle", value.Text),
			})
		}
		if login == "" || seen[login] {
			continue
		}
		seen[login] = true
		out = append(out, login)
	}
	if len(out) == 0 {
		return nil, warnings
	}
	return out, warnings
}

// mapLabels maps the issue tags onto labels, trimmed and deduplicated in the
// order YouTrack returned them. Tags are free text in YouTrack and labels are
// free text in git-in-track, so there is nothing here that can fail.
func mapLabels(issue youtrack.Issue) []string {
	seen := make(map[string]bool, len(issue.Tags))
	out := make([]string, 0, len(issue.Tags))
	for _, tag := range issue.Tags {
		name := strings.TrimSpace(tag.Name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// mapMilestone reads the version-bundle field that stands in for a milestone
// and returns the version name. An issue assigned to several versions keeps the
// first and warns: a git-in-track item belongs to at most one milestone.
func mapMilestone(issue youtrack.Issue, m FieldMap) (string, []Warning) {
	values, warnings := fieldValues(issue, m.MilestoneField)
	if len(values) == 0 {
		return "", warnings
	}
	for _, extra := range values[1:] {
		warnings = append(warnings, Warning{
			Field:    m.MilestoneField,
			Value:    extra.Text,
			Fallback: values[0].Text,
			Reason:   fmt.Sprintf("the issue is assigned to several versions; %q was kept as the milestone and %q ignored", values[0].Text, extra.Text),
		})
	}
	return values[0].Text, warnings
}

// periodTokenRe matches one "<number><unit>" token of a YouTrack period
// presentation such as "1w 2d 3h 30m".
var periodTokenRe = regexp.MustCompile(`(?i)(\d+)\s*([wdhm])`)

// mapEstimate converts the Estimation period into story points.
//
// YouTrack sends a period either as `minutes`, which is exact, or as a
// `presentation` such as "3d 4h", which is the same duration spelled with the
// project's working week. Minutes win when present. The duration is then
// divided by FieldMap.MinutesPerPoint — one working day by default — and
// rounded to two decimals, which is enough for the half and quarter points
// teams actually use.
//
// A presentation this function cannot parse produces a warning and no
// estimate, never a zero: an estimate of 0 is a statement, and guessing one
// would be worse than leaving the field unset.
func mapEstimate(issue youtrack.Issue, m FieldMap) (*float64, []Warning) {
	value, ok, warnings := firstFieldValue(issue, m.EstimationField)
	if !ok {
		return nil, warnings
	}
	minutes, parsed := periodMinutes(value, m)
	if !parsed {
		return nil, append(warnings, Warning{
			Field:  m.EstimationField,
			Value:  value.Text,
			Reason: fmt.Sprintf("the estimation %q is neither a minutes count nor a period this importer can read, so no estimate was set", value.Text),
		})
	}
	points := math.Round(minutes/m.MinutesPerPoint*100) / 100
	return &points, warnings
}

// periodMinutes returns the duration of a period value in minutes.
func periodMinutes(value Value, m FieldMap) (float64, bool) {
	if value.Raw.Minutes != nil {
		return float64(*value.Raw.Minutes), true
	}
	text := strings.TrimSpace(value.Text)
	if text == "" {
		return 0, false
	}
	matches := periodTokenRe.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return 0, false
	}
	// Refuse a presentation that carries anything besides the tokens, so that
	// "3 days-ish" is a warning rather than a silent 3 days.
	if strings.TrimSpace(periodTokenRe.ReplaceAllString(text, "")) != "" {
		return 0, false
	}
	var total float64
	for _, match := range matches {
		n, err := strconv.ParseFloat(match[1], 64)
		if err != nil {
			return 0, false
		}
		switch strings.ToLower(match[2]) {
		case "w":
			total += n * m.MinutesPerWeek
		case "d":
			total += n * m.MinutesPerDay
		case "h":
			total += n * 60
		default:
			total += n
		}
	}
	return total, true
}

// userHandle returns the handle of a YouTrack user: the login, or the full
// name when the instance did not send one.
func userHandle(u youtrack.User) string {
	if login := strings.TrimSpace(u.Login); login != "" {
		return login
	}
	return strings.TrimSpace(u.FullName)
}
