package core

import (
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
func (p ItemPatch) conflictWith(current *Item) []ConflictField {
	if current == nil {
		return nil
	}
	proposed := current.clone()
	if err := applyPatch(proposed, p); err != nil {
		// A patch the store would refuse anyway has no meaningful field diff;
		// the revisions alone are what the caller gets.
		return nil
	}
	return diffFields(current, proposed)
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
	// The body is compared, never quoted: it can be the whole file, and the
	// caller already holds the version it proposed.
	if strings.TrimRight(current.Body, "\n") != strings.TrimRight(proposed.Body, "\n") {
		out = append(out, ConflictField{Field: "body"})
	}
	return out
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
