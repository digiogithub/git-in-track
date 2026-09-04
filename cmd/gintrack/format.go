package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/digiogithub/git-in-track/internal/core"
)

// render reports a failure to write a command's output. It exists so that the
// I/O error of a renderer reaches the caller with the context of what failed.
func render(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("write the output: %w", err)
}

// dash is what an empty cell shows, so that a column never looks truncated.
const dash = "—"

// orDash returns the value or a dash when it is empty.
func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return dash
	}
	return s
}

// joinOrDash joins a list with commas.
func joinOrDash(values []string) string {
	if len(values) == 0 {
		return dash
	}
	return strings.Join(values, ",")
}

// plural renders a count with the right noun.
func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}

// humanDuration renders a build duration the way `gintrack index` prints it.
func humanDuration(d time.Duration) string {
	switch {
	case d < time.Millisecond:
		return fmt.Sprintf("%dµs", d.Microseconds())
	case d < time.Second:
		return fmt.Sprintf("%dms", d.Milliseconds())
	default:
		return fmt.Sprintf("%.2fs", d.Seconds())
	}
}

// ago renders a timestamp as an age, which is what the item table shows.
func ago(ts core.Timestamp, now time.Time) string {
	if ts.IsZero() {
		return dash
	}
	d := now.Sub(ts.Time)
	switch {
	case d < 0:
		return ts.String()
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 365*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	default:
		return fmt.Sprintf("%dy ago", int(d.Hours()/24/365))
	}
}

// countsByType renders the per-type breakdown of an index build.
func countsByType(byType map[core.ItemType]int) string {
	if len(byType) == 0 {
		return ""
	}
	labels := []struct {
		t    core.ItemType
		one  string
		many string
	}{
		{core.TypeEpic, "epic", "epics"},
		{core.TypeStory, "story", "stories"},
		{core.TypeTask, "task", "tasks"},
		{core.TypeMilestone, "milestone", "milestones"},
	}
	parts := make([]string, 0, len(labels))
	for _, l := range labels {
		if n := byType[l.t]; n > 0 {
			parts = append(parts, plural(n, l.one, l.many))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "(" + strings.Join(parts, ", ") + ")"
}
