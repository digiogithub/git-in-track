package core

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Describing a rejected conditional write.
//
// A stale revision on its own only says "someone else wrote first". What a
// client — and above all an unattended agent — needs in order to decide what to
// do next is the answer to a narrower question: given the bytes that are on
// disk now, what would my write still change? A patch whose every field already
// holds the value it wanted is a change that has already happened, and the
// caller can drop it; a patch that still disagrees names exactly where.
//
// The comparison is deliberately made against the current content rather than
// against the caller's base version: the base is a hash, not a document, so the
// store never has it. This is the strongest statement that can be made from
// what a conditional write actually carries, and it is the useful one.

// conflictWith reports the fields this patch would still change if it were
// applied to the item as it stands on disk now.
//
// An empty result is a promise: every field the patch proposes is already on
// disk, so the caller may drop the write (docs/08 section 7.3). A patch the
// store would refuse anyway — a blank title, an unknown field to unset — can
// never keep that promise, because nothing it asked for can have happened; it
// is reported field by field instead, so a refused change never reads as a
// saved one (GIT-US-0152).
// Implements: GIT-SP-0001.R3, GIT-SP-0001.R4
func (p ItemPatch) conflictWith(current *Item) []ConflictField {
	if current == nil {
		return nil
	}
	proposed := current.clone()
	if err := applyPatch(proposed, p); err != nil {
		return p.carriedFields(current)
	}
	return diffFields(current, proposed)
}

// carriedFields names every field a patch carries, with the value on disk and
// the value the patch asked for, whether or not the two differ. It describes a
// patch that cannot be applied, so it never computes a result: set operations
// are rendered as "+value" and "-value", and structured fields (the body,
// custom, external, inbox) are named but never quoted.
func (p ItemPatch) carriedFields(current *Item) []ConflictField {
	var out []ConflictField
	seen := map[string]bool{}
	add := func(field, currentValue, proposedValue string) {
		if seen[field] {
			return
		}
		seen[field] = true
		out = append(out, ConflictField{Field: field, Current: currentValue, Proposed: proposedValue})
	}
	if p.Title != nil {
		add("title", current.Title, *p.Title)
	}
	if p.Status != nil {
		add("status", string(current.Status), string(*p.Status))
	}
	if p.Priority != nil {
		add("priority", string(current.Priority), string(*p.Priority))
	}
	if p.Parent != nil {
		add("parent", string(current.Parent), string(*p.Parent))
	}
	if p.Milestone != nil {
		add("milestone", string(current.Milestone), string(*p.Milestone))
	}
	if p.Sprint != nil {
		add("sprint", current.Sprint, *p.Sprint)
	}
	if p.Author != nil {
		add("author", current.Author, *p.Author)
	}
	if p.Owner != nil {
		add("owner", current.Owner, *p.Owner)
	}
	if p.Assignees != nil || len(p.AddAssignees) > 0 || len(p.RemoveAssignees) > 0 {
		add("assignees", joinList(current.Assignees), listOps(renderList(p.Assignees), p.AddAssignees, p.RemoveAssignees))
	}
	if p.Labels != nil || len(p.AddLabels) > 0 || len(p.RemoveLabels) > 0 {
		add("labels", joinList(current.Labels), listOps(renderList(p.Labels), p.AddLabels, p.RemoveLabels))
	}
	if p.Estimate != nil {
		add("estimate", renderNumber(current.Estimate), renderNumber(p.Estimate))
	}
	if p.Effort != nil {
		add("effort", renderNumber(current.Effort), renderNumber(p.Effort))
	}
	if p.Spent != nil {
		add("spent", renderNumber(current.Spent), renderNumber(p.Spent))
	}
	if p.Start != nil {
		add("start", current.Start.String(), p.Start.String())
	}
	if p.Due != nil {
		add("due", current.Due.String(), p.Due.String())
	}
	if p.Links != nil || len(p.AddLinks) > 0 || len(p.RemoveLinks) > 0 {
		var replace *string
		if p.Links != nil {
			rendered := renderLinks(*p.Links)
			replace = &rendered
		}
		add("links", renderLinks(current.Links), listOps(replace, linkStrings(p.AddLinks), linkStrings(p.RemoveLinks)))
	}
	if len(p.AddAttachments) > 0 {
		add("attachments", joinList(current.Attachments), listOps(nil, p.AddAttachments, nil))
	}
	if p.Deleted != nil {
		add("deleted", strconv.FormatBool(current.Deleted), strconv.FormatBool(*p.Deleted))
	}
	if p.External != nil || len(p.AddExternal) > 0 || len(p.RemoveExternal) > 0 {
		add("external", "", "")
	}
	if p.Inbox != nil {
		add("inbox", "", "")
	}
	if len(p.Custom) > 0 {
		add("custom", "", "")
	}
	if p.Body != nil || p.BodyAppend != "" {
		add("body", "", "")
	}
	for _, field := range p.Unset {
		add(field, "", "")
	}
	return out
}

// renderList renders an optional list replacement; nil means "no replacement".
func renderList(values *[]string) *string {
	if values == nil {
		return nil
	}
	rendered := joinList(*values)
	return &rendered
}

// listOps renders the list operations of a patch: the replacement first, then
// each addition as "+value" and each removal as "-value".
func listOps(replace *string, added, removed []string) string {
	var parts []string
	if replace != nil && *replace != "" {
		parts = append(parts, *replace)
	}
	for _, v := range added {
		parts = append(parts, "+"+v)
	}
	for _, v := range removed {
		parts = append(parts, "-"+v)
	}
	return strings.Join(parts, ", ")
}

// linkStrings renders links one per value.
func linkStrings(links []Link) []string {
	out := make([]string, 0, len(links))
	for _, l := range links {
		out = append(out, fmt.Sprintf("%s %s", l.Kind, l.Target))
	}
	return out
}

// statusIntent builds the intent of a move: one field, the target status.
func statusIntent(status Status) conflictIntent {
	return func(current *Item) []ConflictField {
		if current == nil || current.Status == status {
			return nil
		}
		return []ConflictField{{
			Field: "status", Current: string(current.Status), Proposed: string(status),
		}}
	}
}

// clone copies an item deeply enough that applying a patch to the copy cannot
// reach the original: every field a patch may touch is either a value or a
// freshly allocated slice or map.
func (it *Item) clone() *Item {
	out := *it
	out.Assignees = append([]string(nil), it.Assignees...)
	out.Labels = append([]string(nil), it.Labels...)
	out.Attachments = append([]string(nil), it.Attachments...)
	out.Links = append([]Link(nil), it.Links...)
	if it.Estimate != nil {
		v := *it.Estimate
		out.Estimate = &v
	}
	if it.Effort != nil {
		v := *it.Effort
		out.Effort = &v
	}
	if it.Spent != nil {
		v := *it.Spent
		out.Spent = &v
	}
	if it.Custom != nil {
		out.Custom = make(map[string]any, len(it.Custom))
		for k, v := range it.Custom {
			out.Custom[k] = v
		}
	}
	if it.Extra != nil {
		out.Extra = make(map[string]any, len(it.Extra))
		for k, v := range it.Extra {
			out.Extra[k] = v
		}
	}
	return &out
}

// diffFields lists the front-matter fields and the body that differ between two
// versions of one item, in a fixed order so that two identical conflicts render
// identically. Derived fields (path, rev, updated) are never reported: they are
// consequences of a write, not the subject of one.
// Implements: GIT-SP-0001.R3
func diffFields(current, proposed *Item) []ConflictField {
	var out []ConflictField
	add := func(field, currentValue, proposedValue string) {
		if currentValue == proposedValue {
			return
		}
		out = append(out, ConflictField{Field: field, Current: currentValue, Proposed: proposedValue})
	}
	add("title", current.Title, proposed.Title)
	add("status", string(current.Status), string(proposed.Status))
	add("priority", string(current.Priority), string(proposed.Priority))
	add("parent", string(current.Parent), string(proposed.Parent))
	add("milestone", string(current.Milestone), string(proposed.Milestone))
	add("sprint", current.Sprint, proposed.Sprint)
	add("author", current.Author, proposed.Author)
	add("owner", current.Owner, proposed.Owner)
	add("assignees", joinList(current.Assignees), joinList(proposed.Assignees))
	add("labels", joinList(current.Labels), joinList(proposed.Labels))
	add("estimate", renderNumber(current.Estimate), renderNumber(proposed.Estimate))
	add("effort", renderNumber(current.Effort), renderNumber(proposed.Effort))
	add("spent", renderNumber(current.Spent), renderNumber(proposed.Spent))
	add("start", current.Start.String(), proposed.Start.String())
	add("due", current.Due.String(), proposed.Due.String())
	add("links", renderLinks(current.Links), renderLinks(proposed.Links))
	add("attachments", joinList(current.Attachments), joinList(proposed.Attachments))
	add("deleted", strconv.FormatBool(current.Deleted), strconv.FormatBool(proposed.Deleted))
	// Structured fields are compared, never quoted, like the body: a field a
	// patch can change must be able to appear here, or a stale write to it
	// would come back with an empty list and read as already applied.
	for _, f := range []struct {
		name              string
		current, proposed any
	}{
		{"external", current.External, proposed.External},
		{"inbox", current.Inbox, proposed.Inbox},
		{"custom", current.Custom, proposed.Custom},
	} {
		if !sameEncoding(f.current, f.proposed) {
			out = append(out, ConflictField{Field: f.name})
		}
	}
	// The body is compared, never quoted: it can be the whole file, and the
	// caller already holds the version it proposed.
	if strings.TrimRight(current.Body, "\n") != strings.TrimRight(proposed.Body, "\n") {
		out = append(out, ConflictField{Field: "body"})
	}
	return out
}

// sameEncoding compares two structured values by their JSON encoding, which
// sorts map keys and treats nil and empty collections alike once normalized.
func sameEncoding(a, b any) bool {
	ja, errA := json.Marshal(a)
	jb, errB := json.Marshal(b)
	if errA != nil || errB != nil {
		return false
	}
	return normalizeEmpty(string(ja)) == normalizeEmpty(string(jb))
}

// normalizeEmpty folds the encodings of an absent value into one.
func normalizeEmpty(s string) string {
	switch s {
	case "null", "[]", "{}":
		return ""
	}
	return s
}

// joinList renders a list field as a comma-separated value.
func joinList(values []string) string { return strings.Join(values, ", ") }

// renderNumber renders an optional number, empty when it is unset.
func renderNumber(value *float64) string {
	if value == nil {
		return ""
	}
	return strconv.FormatFloat(*value, 'f', -1, 64)
}

// renderLinks renders the typed relations of an item.
func renderLinks(links []Link) string {
	if len(links) == 0 {
		return ""
	}
	parts := make([]string, 0, len(links))
	for _, l := range links {
		parts = append(parts, fmt.Sprintf("%s %s", l.Kind, l.Target))
	}
	return strings.Join(parts, ", ")
}
