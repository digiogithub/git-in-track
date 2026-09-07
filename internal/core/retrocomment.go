package core

import "strings"

// This file is the discussion half of a retrospective: the remarks a room
// leaves on the notes and themes it is looking at during the `discussing`
// stage (docs/04 section 9.1, step 5).
//
// A comment is front matter rather than body prose for the same reason a note
// is one bullet: one comment is one block of consecutive lines, so two people
// commenting at the same time touch different lines and git merges both sides.
// The body stays a document a human wrote; the discussion stays data the app
// can render next to the card it belongs to.

// RetroComment is one remark attached to a note or to a theme. Exactly one of
// Note and Theme is set: a comment that points at nothing is reported by
// Validate.
type RetroComment struct {
	ID string `yaml:"id" json:"id"`
	// Note is the id of the sticky note this comment discusses.
	Note string `yaml:"note,omitempty" json:"note,omitempty"`
	// Theme is the id of the theme this comment discusses.
	Theme string `yaml:"theme,omitempty" json:"theme,omitempty"`
	// Author is the handle that wrote it, absent on an anonymous retro.
	Author  string    `yaml:"author,omitempty" json:"author,omitempty"`
	Text    string    `yaml:"text" json:"text"`
	Created Timestamp `yaml:"created,omitempty" json:"created,omitempty"`
}

// Target is the note or theme id the comment hangs off, empty when it hangs
// off neither.
func (c RetroComment) Target() string {
	if c.Note != "" {
		return c.Note
	}
	return c.Theme
}

// Comment returns the comment with an id, and whether there is one.
func (r *Retro) Comment(id string) (*RetroComment, bool) {
	for i := range r.Comments {
		if r.Comments[i].ID == id {
			return &r.Comments[i], true
		}
	}
	return nil, false
}

// NextCommentID allocates the next free `cN` id of this retro.
func (r *Retro) NextCommentID() string {
	used := make([]string, 0, len(r.Comments))
	for _, c := range r.Comments {
		used = append(used, c.ID)
	}
	return nextLocalID("c", used)
}

// AddComment appends a comment, allocating its id and stamping it with the
// store clock's now when the caller left it zero.
func (r *Retro) AddComment(c RetroComment) RetroComment {
	if c.ID == "" {
		c.ID = r.NextCommentID()
	}
	c.Text = strings.TrimSpace(c.Text)
	r.Comments = append(r.Comments, c)
	return c
}

// RemoveComment drops a comment and reports whether it was there.
func (r *Retro) RemoveComment(id string) bool {
	for i := range r.Comments {
		if r.Comments[i].ID != id {
			continue
		}
		r.Comments = append(r.Comments[:i:i], r.Comments[i+1:]...)
		return true
	}
	return false
}

// removeCommentsOf drops every comment pointing at a note or theme that no
// longer exists, so a comment never dangles off a card the room deleted.
func (r *Retro) removeCommentsOf(target string) {
	if target == "" {
		return
	}
	kept := make([]RetroComment, 0, len(r.Comments))
	for _, c := range r.Comments {
		if c.Target() == target {
			continue
		}
		kept = append(kept, c)
	}
	r.Comments = kept
}

// CommentsOn returns the comments attached to one note or theme, in the order
// the file lists them.
func (r *Retro) CommentsOn(target string) []RetroComment {
	out := []RetroComment{}
	for _, c := range r.Comments {
		if c.Target() == target {
			out = append(out, c)
		}
	}
	return out
}

// writeRetroComments emits the discussion, one field per line so that a second
// comment on the same card is an append rather than a rewrite.
func writeRetroComments(w *fmWriter, comments []RetroComment) {
	if len(comments) == 0 {
		return
	}
	w.b.WriteString("comments:\n")
	for _, c := range comments {
		w.b.WriteString("  - id: " + yamlFlowString(c.ID) + "\n")
		if c.Note != "" {
			w.b.WriteString("    note: " + yamlFlowString(c.Note) + "\n")
		}
		if c.Theme != "" {
			w.b.WriteString("    theme: " + yamlFlowString(c.Theme) + "\n")
		}
		if c.Author != "" {
			w.b.WriteString("    author: " + yamlFlowString(c.Author) + "\n")
		}
		w.b.WriteString("    text: " + yamlString(c.Text) + "\n")
		if !c.Created.IsZero() {
			w.b.WriteString("    created: " + c.Created.String() + "\n")
		}
	}
}
