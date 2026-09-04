package core

import (
	"fmt"
	"regexp"
	"strings"
)

// This file is the body half of a retrospective: the three collection sections
// and the action checklist are structured data written as ordinary Markdown, so
// that the same file is a working retro board in the app and a readable
// document in a text editor or on the git host (docs/04 section 9.1).
//
// Two properties matter and both come from the same decision — one entry per
// line:
//
//   - Every note and every action is exactly one bullet. Two participants
//     adding notes at the same time therefore touch different lines, and git
//     merges the two sides without losing an entry (AC "concurrent editing").
//   - Every section the tool does not own is preserved verbatim, in place. A
//     retro can be facilitated on a whiteboard and transcribed afterwards, and
//     the discussion somebody wrote by hand is never rewritten.

// retroHeadingRE matches a level-2 body heading. Deeper headings belong to the
// content of the section they sit in.
var retroHeadingRE = regexp.MustCompile(`^##\s+(.*\S)\s*$`)

// retroBulletRE matches a list item and captures its text.
var retroBulletRE = regexp.MustCompile(`^-\s+(.*)$`)

// retroNoteIDRE matches the `(n1)` prefix a note bullet carries.
var retroNoteIDRE = regexp.MustCompile(`^\(([a-z0-9-]{1,16})\)\s+(.*)$`)

// retroHandleRE is the shape of the handle a note bullet is attributed to
// (docs/04 section 3.2).
var retroHandleRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)

// retroAttribution is the separator between a note and the handle that wrote
// it. It is an em dash, as the example of docs/04 section 9.4 writes it.
const retroAttribution = " — "

// retroActionsHeading is the body section the improvement actions are mirrored
// into as a task list.
const retroActionsHeading = "Actions"

// retroSection is one level-2 section of a retro body.
type retroSection struct {
	// heading is the text of the `## ` line, kept verbatim so that a section
	// titled "Went well 🎉" survives a round trip.
	heading string
	// category is set on the three collection sections; the section's bullets
	// are then notes and are re-rendered from them.
	category RetroCategory
	// actions marks the section that mirrors `actions[]` as a task list.
	actions bool
	// preamble is the content before the first bullet of an owned section, and
	// the whole content of a preserved one.
	preamble []string
	// notes are the bullets of a collection section.
	notes []RetroNote
}

// retroBody is a parsed retro body: what comes before the first heading, then
// the sections in the order the file writes them.
type retroBody struct {
	preamble []string
	sections []retroSection
}

// notes returns every note of the body, in document order.
func (b retroBody) notes() []RetroNote {
	out := []RetroNote{}
	for _, s := range b.sections {
		out = append(out, s.notes...)
	}
	return out
}

// categoryOfHeading maps a section heading to the collection category it holds,
// or to the action checklist. Matching is case-insensitive and tolerates the
// singular, because a team renames its columns and the file must keep working.
func categoryOfHeading(heading string) (RetroCategory, bool) {
	switch strings.ToLower(strings.TrimSpace(heading)) {
	case "went well", "what went well":
		return CategoryWentWell, false
	case "to improve", "what to improve", "improve":
		return CategoryToImprove, false
	case "puzzles", "puzzle", "questions":
		return CategoryPuzzle, false
	case "actions", "action items":
		return "", true
	}
	return "", false
}

// parseRetroBody splits a body into sections and reads the bullets of the three
// collection sections as notes.
func parseRetroBody(body string) retroBody {
	var out retroBody
	current := -1
	for _, line := range strings.Split(body, "\n") {
		if m := retroHeadingRE.FindStringSubmatch(line); m != nil {
			category, actions := categoryOfHeading(m[1])
			out.sections = append(out.sections, retroSection{
				heading: m[1], category: category, actions: actions,
			})
			current = len(out.sections) - 1
			continue
		}
		if current < 0 {
			out.preamble = append(out.preamble, line)
			continue
		}
		section := &out.sections[current]
		if section.category == "" {
			// A preserved section — including the action checklist, which is
			// regenerated from `actions[]` and whose bullets are therefore
			// dropped here rather than kept twice.
			if !section.actions || retroBulletRE.FindStringSubmatch(line) == nil {
				section.preamble = append(section.preamble, line)
			}
			continue
		}
		m := retroBulletRE.FindStringSubmatch(line)
		if m == nil {
			if len(section.notes) > 0 && strings.TrimSpace(line) != "" {
				// A wrapped bullet: fold the continuation back onto its note.
				last := &section.notes[len(section.notes)-1]
				last.Text = strings.TrimSpace(last.Text + " " + strings.TrimSpace(line))
				continue
			}
			if len(section.notes) == 0 {
				section.preamble = append(section.preamble, line)
			}
			continue
		}
		section.notes = append(section.notes, parseRetroNote(section.category, m[1]))
	}
	for i := range out.sections {
		out.sections[i].preamble = trimBlankEdges(out.sections[i].preamble)
	}
	out.preamble = trimBlankEdges(out.preamble)
	return out
}

// parseRetroNote reads one collection bullet: an optional `(n1)` id, the text,
// and an optional trailing `— handle` attribution.
func parseRetroNote(category RetroCategory, text string) RetroNote {
	note := RetroNote{Category: category}
	if m := retroNoteIDRE.FindStringSubmatch(text); m != nil {
		note.ID = m[1]
		text = m[2]
	}
	if idx := strings.LastIndex(text, retroAttribution); idx >= 0 {
		candidate := strings.TrimSpace(text[idx+len(retroAttribution):])
		if retroHandleRE.MatchString(candidate) {
			note.Author = candidate
			text = text[:idx]
		}
	}
	note.Text = strings.TrimSpace(text)
	return note
}

// renderRetroNote writes one collection bullet back.
func renderRetroNote(n RetroNote, anonymous bool) string {
	var b strings.Builder
	b.WriteString("- ")
	if n.ID != "" {
		b.WriteString("(" + n.ID + ") ")
	}
	b.WriteString(n.Text)
	if n.Author != "" && !anonymous {
		b.WriteString(retroAttribution + n.Author)
	}
	return b.String()
}

// renderRetroAction writes one improvement action as a task-list bullet. The
// checkbox is the retro's own bookkeeping; for a promoted action the task in
// the project repository is the truth and the UI shows it live (R-RETRO-1).
func renderRetroAction(a RetroAction) string {
	box := "[ ]"
	if a.State() == ActionDone {
		box = "[x]"
	}
	line := fmt.Sprintf("- %s %s — %s", box, a.ID, a.Title)
	var meta []string
	if a.Owner != "" {
		meta = append(meta, a.Owner)
	}
	if !a.Due.IsZero() {
		meta = append(meta, a.Due.String())
	}
	if a.State() == ActionDropped {
		meta = append(meta, "dropped")
	}
	if len(meta) > 0 {
		line += " (" + strings.Join(meta, ", ") + ")"
	}
	if a.Task != "" {
		line += " → `" + a.Task + "`"
	}
	return line
}

// renderRetroBody rebuilds the body: the sections in the order the file already
// writes them, with the collection lists and the action checklist regenerated
// from the structured state and every other section written back verbatim.
func renderRetroBody(r *Retro) string {
	body := r.body
	if len(body.sections) == 0 && len(body.preamble) == 0 && len(r.Notes) == 0 && len(r.Actions) == 0 {
		return strings.Trim(r.Body, "\n")
	}
	var out []string
	out = append(out, body.preamble...)
	seen := map[RetroCategory]bool{}
	hasActions := false
	for _, section := range body.sections {
		if section.category != "" {
			seen[section.category] = true
		}
		if section.actions {
			hasActions = true
		}
		out = appendRetroSection(out, r, section)
	}
	// A category somebody started writing into after the file was created gets
	// its section appended in the canonical order.
	for _, category := range retroCategories {
		if seen[category] || len(r.NotesOf(category)) == 0 {
			continue
		}
		out = appendRetroSection(out, r, retroSection{
			heading: retroSectionTitles[category], category: category,
		})
	}
	if !hasActions && len(r.Actions) > 0 {
		out = appendRetroSection(out, r, retroSection{heading: retroActionsHeading, actions: true})
	}
	return strings.Trim(strings.Join(out, "\n"), "\n")
}

// appendRetroSection writes one section and the blank line before it.
func appendRetroSection(out []string, r *Retro, section retroSection) []string {
	if len(out) > 0 {
		out = append(out, "")
	}
	out = append(out, "## "+section.heading)
	preamble := trimBlankEdges(section.preamble)
	if len(preamble) > 0 {
		out = append(out, "")
		out = append(out, preamble...)
	}
	var list []string
	switch {
	case section.category != "":
		for _, note := range r.NotesOf(section.category) {
			list = append(list, renderRetroNote(note, r.Anonymous))
		}
	case section.actions:
		for _, action := range r.Actions {
			list = append(list, renderRetroAction(action))
		}
	default:
		return out
	}
	if len(list) > 0 {
		out = append(out, "")
		out = append(out, list...)
	}
	return out
}

// trimBlankEdges drops the leading and trailing blank lines of a block.
func trimBlankEdges(lines []string) []string {
	start, end := 0, len(lines)
	for start < end && strings.TrimSpace(lines[start]) == "" {
		start++
	}
	for end > start && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	return lines[start:end]
}

// AddNote appends a sticky note to a collection section, allocating its id and
// creating the section when the file has none yet. It returns the note as it
// was stored.
func (r *Retro) AddNote(category RetroCategory, text, author string) RetroNote {
	note := RetroNote{ID: r.NextNoteID(), Category: category, Text: strings.TrimSpace(text)}
	if !r.Anonymous {
		note.Author = author
	}
	section := r.section(category)
	section.notes = append(section.notes, note)
	r.syncNotes()
	return note
}

// SetBody replaces the whole body and re-reads the notes it carries. It is how
// a new retro is seeded with the sections a facilitator starts from.
func (r *Retro) SetBody(body string) {
	r.Body = body
	r.body = parseRetroBody(body)
	r.Notes = r.body.notes()
}

// ValidRetroLocalID reports whether s is a usable note, theme or action id:
// unique within one retro and short enough to read inside a body bullet
// (docs/04 section 9.2).
func ValidRetroLocalID(s string) bool { return retroLocalIDRE.MatchString(s) }

// UpdateNote edits the text, the author or the category of one note and reports
// whether it was there. Moving a note between categories is how the room
// reclassifies a sticky mid-session.
func (r *Retro) UpdateNote(id string, text, author *string, category RetroCategory) bool {
	for si := range r.body.sections {
		for ni := range r.body.sections[si].notes {
			note := &r.body.sections[si].notes[ni]
			if note.ID != id {
				continue
			}
			if text != nil {
				note.Text = strings.TrimSpace(*text)
			}
			if author != nil {
				note.Author = *author
			}
			if category == "" || category == note.Category {
				r.syncNotes()
				return true
			}
			moved := *note
			moved.Category = category
			r.removeNoteAt(si, ni)
			target := r.section(category)
			target.notes = append(target.notes, moved)
			r.syncNotes()
			return true
		}
	}
	return false
}

// RemoveNote drops a note and reports whether it was there. It also drops the
// note from every theme that grouped it, so a theme never points at nothing.
func (r *Retro) RemoveNote(id string) bool {
	for si := range r.body.sections {
		for ni := range r.body.sections[si].notes {
			if r.body.sections[si].notes[ni].ID != id {
				continue
			}
			r.removeNoteAt(si, ni)
			for ti := range r.Themes {
				kept := make([]string, 0, len(r.Themes[ti].Notes))
				for _, note := range r.Themes[ti].Notes {
					if note != id {
						kept = append(kept, note)
					}
				}
				r.Themes[ti].Notes = kept
			}
			r.syncNotes()
			return true
		}
	}
	return false
}

// removeNoteAt deletes one note by its position.
func (r *Retro) removeNoteAt(section, note int) {
	notes := r.body.sections[section].notes
	kept := make([]RetroNote, 0, len(notes)-1)
	kept = append(kept, notes[:note]...)
	kept = append(kept, notes[note+1:]...)
	r.body.sections[section].notes = kept
}

// section returns the section of a category, appending one in canonical order
// when the file has none.
func (r *Retro) section(category RetroCategory) *retroSection {
	for i := range r.body.sections {
		if r.body.sections[i].category == category {
			return &r.body.sections[i]
		}
	}
	fresh := retroSection{heading: retroSectionTitles[category], category: category}
	if fresh.heading == "" {
		fresh.heading = string(category)
	}
	at := len(r.body.sections)
	for i, existing := range r.body.sections {
		if existing.actions || retroCategoryAfter(existing.category, category) {
			at = i
			break
		}
	}
	r.body.sections = append(r.body.sections, retroSection{})
	copy(r.body.sections[at+1:], r.body.sections[at:])
	r.body.sections[at] = fresh
	return &r.body.sections[at]
}

// retroCategoryAfter reports whether existing comes after want in the canonical
// section order. A section that is not a collection section never does.
func retroCategoryAfter(existing, want RetroCategory) bool {
	rank := func(c RetroCategory) int {
		for i, candidate := range retroCategories {
			if candidate == c {
				return i
			}
		}
		return -1
	}
	a, b := rank(existing), rank(want)
	return a >= 0 && b >= 0 && a > b
}

// syncNotes refreshes the derived note list and the rendered body after a body
// edit, so that a caller reads back exactly what a write would store.
func (r *Retro) syncNotes() {
	r.Notes = r.body.notes()
	r.Body = renderRetroBody(r)
}

// AddAction records one improvement action, allocating its id when the caller
// supplies none. An action is a trackable item from the moment it exists: it
// carries an owner, a due date and, once promoted, the task it became.
func (r *Retro) AddAction(action RetroAction) RetroAction {
	if action.ID == "" {
		action.ID = r.NextActionID()
	}
	if action.Status == "" {
		action.Status = ActionProposed
	}
	r.Actions = append(r.Actions, action)
	r.syncNotes()
	return action
}

// RemoveAction drops an action and reports whether it was there.
func (r *Retro) RemoveAction(id string) bool {
	kept := make([]RetroAction, 0, len(r.Actions))
	found := false
	for _, action := range r.Actions {
		if action.ID == id {
			found = true
			continue
		}
		kept = append(kept, action)
	}
	r.Actions = kept
	r.syncNotes()
	return found
}

// Refresh re-renders the body after a caller edited the structured state in
// place — an action's owner, its status, the task it was promoted into. It is
// what keeps the checklist in the body and `actions[]` in the front matter
// saying the same thing.
func (r *Retro) Refresh() { r.syncNotes() }
