package core

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// This file maps a fragment of a spec — a chunk a semantic index returned — back
// onto the requirement block it came from (GIT-US-0118). A semantic backend
// indexes the spec file whole and answers with a chunk of it, never with an
// offset, so the block is found by locating the chunk's text inside the body
// and intersecting that range with the byte ranges the block parser reports.
// Like the rest of the spec layer it is pure text processing (ADR-003).

// RequirementMatch is the requirement block a fragment landed in, and the part
// of the fragment that lies inside that block.
type RequirementMatch struct {
	Block RequirementBlock
	// Start and End are the byte range, in the body, of the fragment clipped
	// to the block: body[Start:End] is the text that matched inside it.
	Start int
	End   int
}

// fragmentProbe is how many bytes of a fragment's head and tail are searched
// for when the fragment as a whole is not found in the body.
const fragmentProbe = 80

// LocateRequirement reports which requirement block of a spec body a fragment
// of that body belongs to. The fragment is located whitespace-insensitively,
// because an indexer may re-flow the text it chunks; when it is not found whole
// — it overlaps text the body no longer holds, or front matter the body never
// held — its head and its tail are located instead. A fragment that spans two
// blocks belongs to the one it overlaps most, the first one on a tie. A
// fragment that lies outside every block (the spec's introduction), or that is
// not in the body at all, belongs to none.
func LocateRequirement(spec ItemID, body, fragment string) (RequirementMatch, bool) {
	start, end, ok := locateFragment(body, fragment)
	if !ok {
		return RequirementMatch{}, false
	}
	var (
		best    RequirementMatch
		overlap int
	)
	for _, blk := range ParseSpecBody(spec, body).Blocks {
		lo, hi := max(start, blk.Start), min(end, blk.End)
		if hi-lo > overlap {
			best, overlap = RequirementMatch{Block: blk, Start: lo, End: hi}, hi-lo
		}
	}
	return best, overlap > 0
}

// collapsedText is a text with every run of white space reduced to one space,
// and the byte offset in the original of every byte it keeps.
type collapsedText struct {
	text   string
	offset []int
}

// collapseSpace builds the collapsed form of s, trimmed at both ends.
func collapseSpace(s string) collapsedText {
	var (
		b      strings.Builder
		offset = make([]int, 0, len(s))
		space  = false
	)
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if unicode.IsSpace(r) {
			space = true
			i += size
			continue
		}
		if space && b.Len() > 0 {
			b.WriteByte(' ')
			offset = append(offset, i)
		}
		space = false
		b.WriteString(s[i : i+size])
		for k := range size {
			offset = append(offset, i+k)
		}
		i += size
	}
	return collapsedText{text: b.String(), offset: offset}
}

// span converts a range of the collapsed text into one of the original.
func (c collapsedText) span(from, to int) (start, end int) {
	return c.offset[from], c.offset[to-1] + 1
}

// locateFragment finds a fragment in a body and returns its byte range there.
func locateFragment(body, fragment string) (start, end int, ok bool) {
	frag := collapseSpace(fragment).text
	if frag == "" {
		return 0, 0, false
	}
	cb := collapseSpace(body)
	if i := strings.Index(cb.text, frag); i >= 0 {
		s, e := cb.span(i, i+len(frag))
		return s, e, true
	}
	if len(frag) <= fragmentProbe {
		return 0, 0, false
	}
	head := frag[:runeCut(frag, fragmentProbe)]
	tail := frag[runeStart(frag, len(frag)-fragmentProbe):]
	hi := strings.Index(cb.text, head)
	ti := -1
	if hi >= 0 {
		if j := strings.Index(cb.text[hi:], tail); j >= 0 {
			ti = hi + j
		}
	} else {
		ti = strings.Index(cb.text, tail)
	}
	switch {
	case hi >= 0 && ti >= 0:
		s, _ := cb.span(hi, hi+len(head))
		_, e := cb.span(ti, ti+len(tail))
		return s, e, true
	case hi >= 0:
		s, e := cb.span(hi, hi+len(head))
		return s, e, true
	case ti >= 0:
		s, e := cb.span(ti, ti+len(tail))
		return s, e, true
	}
	return 0, 0, false
}

// runeCut returns the largest rune boundary of s not past n.
func runeCut(s string, n int) int {
	for n > 0 && n < len(s) && !utf8.RuneStart(s[n]) {
		n--
	}
	return n
}

// runeStart returns the smallest rune boundary of s not before n.
func runeStart(s string, n int) int {
	for n < len(s) && !utf8.RuneStart(s[n]) {
		n++
	}
	return n
}
