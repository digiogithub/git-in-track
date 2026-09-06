package core

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// This file owns the one operation the detail view needs on a body it does not
// otherwise edit: flipping a single task-list checkbox.
//
// The contract is deliberately narrow. A toggle addresses one line of the body
// by its 1-based line number, and the rewrite replaces the single byte between
// the brackets. Nothing else in the document is parsed, reflowed or
// re-serialized, so a body that round-trips through the emitter today keeps
// round-tripping byte for byte after a toggle (docs/05-web-app.md section 8.2).

// ErrNotTaskListItem reports a line that is not a task-list item: out of range,
// prose, or a `- [ ]` inside a fenced code block, which Markdown renders as
// code and never as a checkbox.
var ErrNotTaskListItem = errors.New("the line is not a task-list item")

// TaskListItemMismatchCode is the RFC 7807 machine code of that problem. The
// client that sent the line number is looking at a stale body, so the fix is to
// re-read the item rather than to retry.
const TaskListItemMismatchCode = "task_list_mismatch"

// taskListItemPattern matches a GitHub-flavored task-list marker: an optional
// indent, a bullet or an ordered marker, at least one space, and the checkbox
// itself. The checkbox must be followed by whitespace or end the line, which is
// what separates `- [x] done` from `- [x]done`, a literal bracket pair.
var taskListItemPattern = regexp.MustCompile(`^([ \t]*)([-*+]|\d{1,9}[.)])([ \t]+)\[([ xX])\]([ \t]|$)`)

// fencePattern matches the opening or closing line of a fenced code block.
var fencePattern = regexp.MustCompile("^[ \t]*(```+|~~~+)")

// TaskListItem is one checkbox of a Markdown body.
type TaskListItem struct {
	// Line is the 1-based line the marker sits on, which is what the web app
	// stamps on the rendered checkbox and sends back to toggle it.
	Line int `json:"line"`
	// Checked is the current state: `- [x]` and `- [X]` are both checked.
	Checked bool `json:"checked"`
	// Text is the label after the checkbox, trimmed. It is what a caller shows
	// in a confirmation or a log line; it is never written back.
	Text string `json:"text"`
	// Indent is the leading whitespace of the marker, so a caller can tell a
	// nested criterion from a top-level one.
	Indent string `json:"indent,omitempty"`
	// column is the 0-based byte offset of the "[" within the line.
	column int
}

// TaskListItems returns every task-list checkbox of a body, in document order.
//
// Fenced code blocks are skipped: their content is code, and the renderer draws
// no checkbox for it. Everything else is a line-level decision, which is what
// keeps this function and the renderer's numbering in step.
func TaskListItems(body string) []TaskListItem {
	var out []TaskListItem
	fence := ""
	for i, line := range splitBodyLines(body) {
		text := strings.TrimRight(line, "\r\n")
		if marker := fencePattern.FindStringSubmatch(text); marker != nil {
			switch {
			case fence == "":
				fence = marker[1][:3]
			case strings.HasPrefix(marker[1], fence):
				fence = ""
			}
			continue
		}
		if fence != "" {
			continue
		}
		m := taskListItemPattern.FindStringSubmatchIndex(text)
		if m == nil {
			continue
		}
		// Submatch 4 is the character between the brackets, so its start minus
		// one is the "[" itself.
		box := m[8]
		out = append(out, TaskListItem{
			Line:    i + 1,
			Checked: text[box] != ' ',
			Text:    strings.TrimSpace(text[m[9]+1:]),
			Indent:  text[m[2]:m[3]],
			column:  box - 1,
		})
	}
	return out
}

// SetTaskListItem returns the body with the checkbox on line set to checked.
//
// It rewrites exactly one byte — the character between the brackets — and
// leaves every other byte of the document untouched, the line's own terminator
// included. Toggling a checkbox that already holds the requested state returns
// the body unchanged, so a double click is not a write.
func SetTaskListItem(body string, line int, checked bool) (string, error) {
	item, ok := findTaskListItem(body, line)
	if !ok {
		return "", fmt.Errorf("toggle line %d: %w", line, ErrNotTaskListItem)
	}
	if item.Checked == checked {
		return body, nil
	}

	lines := splitBodyLines(body)
	target := lines[line-1]
	mark := " "
	if checked {
		mark = "x"
	}
	lines[line-1] = target[:item.column+1] + mark + target[item.column+2:]
	return strings.Join(lines, ""), nil
}

// findTaskListItem looks a line up among the task-list items of a body.
func findTaskListItem(body string, line int) (TaskListItem, bool) {
	for _, item := range TaskListItems(body) {
		if item.Line == line {
			return item, true
		}
	}
	return TaskListItem{}, false
}

// splitBodyLines cuts a body into lines that keep their terminators, so that
// joining them again reproduces the input byte for byte — CRLF, a missing
// final newline and all.
func splitBodyLines(body string) []string {
	if body == "" {
		return []string{""}
	}
	lines := strings.SplitAfter(body, "\n")
	// SplitAfter yields a trailing empty element when the body ends with a
	// newline; that element is not a line of the document.
	if last := len(lines) - 1; lines[last] == "" {
		lines = lines[:last]
	}
	return lines
}
