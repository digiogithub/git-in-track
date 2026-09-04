package core

// Three-way text merge (docs/06-git-sync.md section 5.3).
//
// The body of an item is free Markdown, so a conflict in it is a text problem
// and not a data-model problem. This file holds the text half: a line-based
// diff3 that reports what each side did to each region of the base, so the UI
// can show mine / theirs / base per hunk instead of raw conflict markers.
//
// It lives in internal/core, not in internal/gitops, because browser-only mode
// has no git that could merge for it: the same code runs natively and under
// GOOS=js (AGENTS.md, "keep internal/core free of OS-specific code").

import "strings"

// maxDiffCells bounds the LCS table. Bodies are prose, so the bound is never
// reached in practice; a generated file that does reach it degrades to one
// unstable region, which the UI still resolves with keep-mine / keep-theirs.
const maxDiffCells = 4_000_000

// TextRegion is one region of a three-way comparison. A stable region is text
// all three sides agree on; an unstable one is a region at least one side
// changed, with the three versions kept side by side.
type TextRegion struct {
	// Stable reports a region no side changed.
	Stable bool
	// Lines is the content of a stable region.
	Lines []string
	// Base, Ours and Theirs are the three versions of an unstable region.
	Base, Ours, Theirs []string
}

// splitLines splits text into lines, dropping the trailing empty element a
// final newline produces so that a merge does not invent a blank last line.
func splitLines(text string) []string {
	if text == "" {
		return nil
	}
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// joinLines is the inverse of splitLines.
func joinLines(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n")
}

// Diff3 compares ours and theirs against their common base and returns the
// regions in base order. It is the classic diff3 walk: the sync points are the
// base lines both sides kept, and everything between two sync points is one
// unstable region.
func Diff3(base, ours, theirs []string) []TextRegion {
	ourMatch := matchLines(base, ours)
	theirMatch := matchLines(base, theirs)

	regions := make([]TextRegion, 0, 8)
	stable := make([]string, 0, len(base))
	flush := func() {
		if len(stable) > 0 {
			regions = append(regions, TextRegion{Stable: true, Lines: append([]string(nil), stable...)})
			stable = stable[:0]
		}
	}

	i, ourAt, theirAt := 0, 0, 0
	for i < len(base) {
		o, okOurs := ourMatch[i]
		t, okTheirs := theirMatch[i]
		if okOurs && okTheirs && o == ourAt && t == theirAt {
			stable = append(stable, base[i])
			i, ourAt, theirAt = i+1, ourAt+1, theirAt+1
			continue
		}
		next, nextOurs, nextTheirs := nextSyncPoint(base, ours, theirs, ourMatch, theirMatch, i, ourAt, theirAt)
		region := TextRegion{
			Base:   append([]string(nil), base[i:next]...),
			Ours:   append([]string(nil), ours[ourAt:nextOurs]...),
			Theirs: append([]string(nil), theirs[theirAt:nextTheirs]...),
		}
		// Trim the lines all three sides share at the edges of the region:
		// a tight hunk is one the user can read, and a hunk that is only a
		// context line wider is one they have to diff by eye.
		lead, tail := trimShared(&region)
		stable = append(stable, lead...)
		if len(region.Base) > 0 || len(region.Ours) > 0 || len(region.Theirs) > 0 {
			flush()
			regions = append(regions, region)
		}
		stable = append(stable, tail...)
		i, ourAt, theirAt = next, nextOurs, nextTheirs
	}
	flush()
	if ourAt < len(ours) || theirAt < len(theirs) {
		regions = append(regions, TextRegion{
			Ours:   append([]string(nil), ours[ourAt:]...),
			Theirs: append([]string(nil), theirs[theirAt:]...),
		})
	}
	return regions
}

// trimShared moves the lines every side agrees on out of an unstable region,
// returning what belongs before it and what belongs after it.
func trimShared(region *TextRegion) (lead, tail []string) {
	for len(region.Base) > 0 && len(region.Ours) > 0 && len(region.Theirs) > 0 &&
		region.Base[0] == region.Ours[0] && region.Base[0] == region.Theirs[0] {
		lead = append(lead, region.Base[0])
		region.Base, region.Ours, region.Theirs = region.Base[1:], region.Ours[1:], region.Theirs[1:]
	}
	for len(region.Base) > 0 && len(region.Ours) > 0 && len(region.Theirs) > 0 &&
		last(region.Base) == last(region.Ours) && last(region.Base) == last(region.Theirs) {
		tail = append([]string{last(region.Base)}, tail...)
		region.Base = region.Base[:len(region.Base)-1]
		region.Ours = region.Ours[:len(region.Ours)-1]
		region.Theirs = region.Theirs[:len(region.Theirs)-1]
	}
	return lead, tail
}

// last is the final element of a non-empty slice.
func last(lines []string) string { return lines[len(lines)-1] }

// nextSyncPoint finds the first base line at or after i that both sides kept in
// the order they are being walked in, and reports where each side stands there.
// With no such line the region runs to the end of all three texts.
func nextSyncPoint(base, ours, theirs []string, ourMatch, theirMatch map[int]int, i, ourAt, theirAt int) (nextBase, nextOurs, nextTheirs int) {
	for j := i + 1; j < len(base); j++ {
		o, okOurs := ourMatch[j]
		t, okTheirs := theirMatch[j]
		if okOurs && okTheirs && o >= ourAt && t >= theirAt {
			return j, o, t
		}
	}
	return len(base), len(ours), len(theirs)
}

// matchLines pairs the lines of a with the lines of b through their longest
// common subsequence: the result maps an index in a to the index in b that is
// the same line.
func matchLines(a, b []string) map[int]int {
	out := make(map[int]int, len(a))
	if len(a) == 0 || len(b) == 0 {
		return out
	}
	// Strip the common prefix and suffix first: it is what keeps a small edit
	// in a long body cheap, and it bounds the table for everything else.
	start := 0
	for start < len(a) && start < len(b) && a[start] == b[start] {
		out[start] = start
		start++
	}
	endA, endB := len(a), len(b)
	for endA > start && endB > start && a[endA-1] == b[endB-1] {
		out[endA-1] = endB - 1
		endA, endB = endA-1, endB-1
	}
	midA, midB := a[start:endA], b[start:endB]
	if len(midA) == 0 || len(midB) == 0 {
		return out
	}
	if len(midA)*len(midB) > maxDiffCells {
		return out
	}
	for ai, bi := range lcsPairs(midA, midB) {
		out[start+ai] = start + bi
	}
	return out
}

// lcsPairs returns the index pairs of a longest common subsequence of a and b.
func lcsPairs(a, b []string) map[int]int {
	rows, cols := len(a)+1, len(b)+1
	table := make([]int, rows*cols)
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				table[i*cols+j] = table[(i+1)*cols+j+1] + 1
				continue
			}
			if table[(i+1)*cols+j] >= table[i*cols+j+1] {
				table[i*cols+j] = table[(i+1)*cols+j]
			} else {
				table[i*cols+j] = table[i*cols+j+1]
			}
		}
	}
	out := make(map[int]int, len(a))
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i] == b[j]:
			out[i] = j
			i, j = i+1, j+1
		case table[(i+1)*cols+j] >= table[i*cols+j+1]:
			i++
		default:
			j++
		}
	}
	return out
}

// sectionOf reports the last Markdown heading at or before line index i, which
// is what labels a hunk in the resolver ("under ## Acceptance Criteria").
func sectionOf(lines []string, i int) string {
	for j := i - 1; j >= 0; j-- {
		line := strings.TrimSpace(lines[j])
		if strings.HasPrefix(line, "#") {
			return line
		}
	}
	return ""
}

// checkboxKey strips the checkbox state from a task-list line, returning the
// text the two states share and whether the line is a task-list item at all.
func checkboxKey(line string) (key string, checked, ok bool) {
	trimmed := strings.TrimLeft(line, " \t")
	indent := line[:len(line)-len(trimmed)]
	for _, bullet := range []string{"- ", "* ", "+ "} {
		rest, found := strings.CutPrefix(trimmed, bullet)
		if !found {
			continue
		}
		switch {
		case strings.HasPrefix(rest, "[ ] "):
			return indent + bullet + rest[4:], false, true
		case strings.HasPrefix(rest, "[x] "), strings.HasPrefix(rest, "[X] "):
			return indent + bullet + rest[4:], true, true
		}
	}
	return "", false, false
}

// checkboxMerge resolves a hunk whose two sides differ only in the state of the
// same acceptance-criteria checkboxes: a criterion someone ticked stays ticked
// (docs/06 section 5.3). It reports false when the hunk is anything else.
func checkboxMerge(ours, theirs []string) ([]string, bool) {
	if len(ours) != len(theirs) || len(ours) == 0 {
		return nil, false
	}
	out := make([]string, 0, len(ours))
	differed := false
	for i := range ours {
		if ours[i] == theirs[i] {
			out = append(out, ours[i])
			continue
		}
		ourKey, ourChecked, ourOK := checkboxKey(ours[i])
		theirKey, theirChecked, theirOK := checkboxKey(theirs[i])
		if !ourOK || !theirOK || ourKey != theirKey {
			return nil, false
		}
		differed = true
		if ourChecked || theirChecked {
			out = append(out, checkedLine(ourKey))
			continue
		}
		out = append(out, ours[i])
	}
	if !differed {
		return nil, false
	}
	return out, true
}

// checkedLine renders a task-list line in the checked state.
func checkedLine(key string) string {
	trimmed := strings.TrimLeft(key, " \t")
	indent := key[:len(key)-len(trimmed)]
	bullet := trimmed[:2]
	return indent + bullet + "[x] " + trimmed[2:]
}
