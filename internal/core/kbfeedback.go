package core

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"
)

// Feedback notes on knowledge-base pages (docs/03 section 14.4, ADR-030).
//
// A reviewer selects text on a rendered page and attaches a note to it. The
// notes are written into the page itself, in one block at the end of the file,
// so that whoever reads the page next — a person or an agent — reads the
// feedback together with the text it is about:
//
//	<!-- gintrack:feedback:begin -->
//
//	---
//
//	## Feedback
//
//	<!-- gintrack:feedback:note id="fb-…" anchor="sha256:…" lines="12-14" author="…" email="…" created="…" -->
//	### Jose F. Rives Lirola feedback: fb-…
//
//	> Lines 12–14:
//	>
//	> <the referenced source lines, verbatim>
//
//	Selected: “<the selected text>”
//
//	<the note>
//
//	<!-- gintrack:feedback:end -->
//
// Every note carries an anchor: the hash of the source lines it refers to. A
// note whose text no longer exists anywhere in the page is dropped the next time
// the page is written through the core (PruneKbFeedback), so feedback never
// outlives the text it was about.

// The markers of the feedback block. They are HTML comments so that a Markdown
// renderer shows nothing of them.
const (
	kbFeedbackBegin      = "<!-- gintrack:feedback:begin -->"
	kbFeedbackEnd        = "<!-- gintrack:feedback:end -->"
	kbFeedbackNotePrefix = "<!-- gintrack:feedback:note "
	kbFeedbackNoteSuffix = "-->"
	kbFeedbackHeading    = "## Feedback"
)

// ErrInvalidFeedback marks a feedback note that cannot be anchored or has no
// text. Hosts report it as an invalid request.
var ErrInvalidFeedback = errors.New("invalid feedback")

// KbFeedbackDraft is one note a reviewer attaches to a page. StartLine and
// EndLine are 1-based lines of the page body — the Markdown after the front
// matter, exactly as KBPage.Body holds it.
type KbFeedbackDraft struct {
	StartLine int    `json:"startLine"`
	EndLine   int    `json:"endLine"`
	Quote     string `json:"quote,omitempty"`
	Note      string `json:"note"`
	// Author is a handle; AuthorName and AuthorEmail the git identity. The
	// block shows AuthorName, else the handle.
	Author      string `json:"author,omitempty"`
	AuthorName  string `json:"authorName,omitempty"`
	AuthorEmail string `json:"authorEmail,omitempty"`
}

// KbFeedbackNote is one note of a page's feedback block.
type KbFeedbackNote struct {
	ID        string    `json:"id"`
	Anchor    string    `json:"anchor"`
	StartLine int       `json:"startLine"`
	EndLine   int       `json:"endLine"`
	Author    string    `json:"author"`
	Email     string    `json:"email,omitempty"`
	Created   Timestamp `json:"created,omitzero"`
	Note      string    `json:"note"`
}

// kbFeedbackEntry is one note together with the lines it occupies in the file.
type kbFeedbackEntry struct {
	note  KbFeedbackNote
	valid bool
	lines []string
}

// kbFeedbackDoc is a page split around its feedback block.
type kbFeedbackDoc struct {
	// head is the front matter and the blank lines before the body, verbatim.
	head string
	// content is the body without the feedback block: what notes anchor to.
	content  []string
	entries  []kbFeedbackEntry
	hasBlock bool
}

var (
	kbFeedbackAttrRE  = regexp.MustCompile(`([a-z]+)="([^"]*)"`)
	kbFeedbackLabelRE = regexp.MustCompile(`^> Lines? \d+(?:–\d+)?:$`)
)

// AddKbFeedback appends notes to the feedback block of a page, creating the
// block when there is none. Notes already in the block are kept, except those
// whose anchored text has disappeared. It refuses a note with no text, or one
// whose lines fall outside the page content (the block itself included).
func AddKbFeedback(data []byte, drafts []KbFeedbackDraft, now Timestamp) ([]byte, []KbFeedbackNote, error) {
	if len(drafts) == 0 {
		return nil, nil, fmt.Errorf("%w: no notes to add", ErrInvalidFeedback)
	}
	doc := parseKbFeedbackDoc(data)
	doc.prune()
	content := trimTrailingBlank(doc.content)

	used := map[string]bool{}
	for _, e := range doc.entries {
		used[e.note.ID] = true
	}
	added := make([]KbFeedbackNote, 0, len(drafts))
	for i, d := range drafts {
		text := strings.TrimSpace(d.Note)
		if text == "" {
			return nil, nil, fmt.Errorf("%w: note %d has no text", ErrInvalidFeedback, i+1)
		}
		if d.StartLine < 1 || d.EndLine < d.StartLine || d.EndLine > len(content) {
			return nil, nil, fmt.Errorf(
				"%w: note %d refers to lines %d-%d, outside the page content (lines 1-%d)",
				ErrInvalidFeedback, i+1, d.StartLine, d.EndLine, len(content))
		}
		source := content[d.StartLine-1 : d.EndLine]
		anchor := kbFeedbackAnchor(source)
		if anchor == "" {
			return nil, nil, fmt.Errorf("%w: note %d refers only to blank lines", ErrInvalidFeedback, i+1)
		}
		note := KbFeedbackNote{
			Anchor:    anchor,
			StartLine: d.StartLine,
			EndLine:   d.EndLine,
			Author:    kbFeedbackAuthor(d),
			Email:     oneLine(d.AuthorEmail),
			Created:   now,
			Note:      text,
		}
		note.ID = kbFeedbackID(note, used)
		used[note.ID] = true
		doc.entries = append(doc.entries, kbFeedbackEntry{
			note:  note,
			valid: true,
			lines: renderKbFeedbackEntry(note, source, d.Quote),
		})
		added = append(added, note)
	}
	return doc.render(), added, nil
}

// PruneKbFeedback drops every feedback note whose anchored text no longer
// appears in the page, and moves the line range of a note whose text moved.
// When no note is left the block is removed. A page without a feedback block
// is returned unchanged, byte for byte.
func PruneKbFeedback(data []byte) []byte {
	if !bytes.Contains(data, []byte(kbFeedbackBegin)) {
		return data
	}
	doc := parseKbFeedbackDoc(data)
	if !doc.hasBlock {
		return data
	}
	doc.prune()
	return doc.render()
}

// ParseKbFeedback lists the notes of a page's feedback block, as written.
func ParseKbFeedback(data []byte) []KbFeedbackNote {
	doc := parseKbFeedbackDoc(data)
	out := make([]KbFeedbackNote, 0, len(doc.entries))
	for _, e := range doc.entries {
		if e.valid {
			out = append(out, e.note)
		}
	}
	return out
}

// splitKbBody separates the front matter (and the blank lines after it) from
// the body, with the same rules ParsePage applies, so that line N of the body
// here is line N of KBPage.Body.
func splitKbBody(data []byte) (head, body string) {
	text := strings.ReplaceAll(string(bytes.TrimPrefix(data, bom)), "\r\n", "\n")
	offset := 0
	if rest, ok := strings.CutPrefix(text, delimiter+"\n"); ok {
		if end := indexClosingFence([]byte(rest)); end >= 0 {
			after := rest[end:]
			if i := strings.IndexByte(after, '\n'); i >= 0 {
				offset = len(delimiter) + 1 + end + i + 1
			} else {
				offset = len(text)
			}
		}
	}
	rest := text[offset:]
	trimmed := strings.TrimLeft(rest, "\n")
	return text[:offset+len(rest)-len(trimmed)], strings.TrimRight(trimmed, "\n")
}

// parseKbFeedbackDoc splits a page around its feedback block. The block is
// recognized only when its begin marker sits outside a code fence and its end
// marker is the last non-blank line of the page, so a page that documents the
// format in a code sample is never mistaken for one that carries feedback.
func parseKbFeedbackDoc(data []byte) kbFeedbackDoc {
	head, body := splitKbBody(data)
	lines := strings.Split(body, "\n")
	doc := kbFeedbackDoc{head: head, content: lines}

	last := len(lines) - 1
	for last >= 0 && strings.TrimSpace(lines[last]) == "" {
		last--
	}
	if last < 0 || strings.TrimSpace(lines[last]) != kbFeedbackEnd {
		return doc
	}
	begin := -1
	fence := ""
	for i := 0; i < last; i++ {
		trimmed := strings.TrimSpace(lines[i])
		if marker := fenceMarker(lines[i]); marker != "" {
			switch {
			case fence == "":
				fence = marker
			case strings.HasPrefix(marker, fence[:1]) && len(marker) >= len(fence):
				fence = ""
			}
			continue
		}
		if fence == "" && trimmed == kbFeedbackBegin {
			begin = i
		}
	}
	if begin < 0 {
		return doc
	}

	doc.hasBlock = true
	doc.content = lines[:begin]
	current := -1
	for _, line := range lines[begin+1 : last] {
		if strings.HasPrefix(strings.TrimSpace(line), kbFeedbackNotePrefix) {
			note, ok := parseKbFeedbackMarker(line)
			doc.entries = append(doc.entries, kbFeedbackEntry{note: note, valid: ok, lines: []string{line}})
			current = len(doc.entries) - 1
			continue
		}
		if current >= 0 {
			doc.entries[current].lines = append(doc.entries[current].lines, line)
		}
	}
	for i := range doc.entries {
		e := &doc.entries[i]
		e.lines = trimTrailingBlank(e.lines)
		e.note.Note = kbFeedbackNoteText(e.lines)
	}
	return doc
}

// fenceMarker returns the run of backticks or tildes that opens or closes a
// fenced code block on this line, or "" when the line is not a fence.
func fenceMarker(line string) string {
	trimmed := strings.TrimLeft(line, " ")
	if len(line)-len(trimmed) > 3 || len(trimmed) < 3 {
		return ""
	}
	c := trimmed[0]
	if c != '`' && c != '~' {
		return ""
	}
	n := 0
	for n < len(trimmed) && trimmed[n] == c {
		n++
	}
	if n < 3 {
		return ""
	}
	return trimmed[:n]
}

// prune drops the notes whose anchor matches no window of the content, and
// moves the others to the matching window closest to where they were.
func (d *kbFeedbackDoc) prune() {
	content := trimTrailingBlank(d.content)
	kept := d.entries[:0]
	for _, e := range d.entries {
		if !e.valid {
			continue
		}
		size := e.note.EndLine - e.note.StartLine + 1
		start, ok := locateKbFeedbackAnchor(content, e.note.Anchor, e.note.StartLine, size)
		if !ok {
			continue
		}
		if start != e.note.StartLine {
			e.note.StartLine = start
			e.note.EndLine = start + size - 1
			e.lines[0] = renderKbFeedbackMarker(e.note)
			for i, line := range e.lines {
				if kbFeedbackLabelRE.MatchString(line) {
					e.lines[i] = "> " + kbFeedbackLinesLabel(e.note.StartLine, e.note.EndLine) + ":"
					break
				}
			}
		}
		kept = append(kept, e)
	}
	d.entries = kept
}

// render writes the page back: head, content, then the block when any note is
// left.
func (d *kbFeedbackDoc) render() []byte {
	content := trimTrailingBlank(d.content)
	var b strings.Builder
	b.WriteString(d.head)
	b.WriteString(strings.Join(content, "\n"))
	if len(d.entries) > 0 {
		if len(content) > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(kbFeedbackBegin + "\n\n---\n\n" + kbFeedbackHeading + "\n")
		for _, e := range d.entries {
			b.WriteString("\n")
			b.WriteString(strings.Join(e.lines, "\n"))
			b.WriteString("\n")
		}
		b.WriteString("\n" + kbFeedbackEnd)
	}
	b.WriteString("\n")
	return []byte(b.String())
}

// locateKbFeedbackAnchor finds the window of size lines whose anchor matches,
// preferring the one closest to the line the note was written against.
func locateKbFeedbackAnchor(content []string, anchor string, near, size int) (int, bool) {
	if anchor == "" || size <= 0 || size > len(content) {
		return 0, false
	}
	if near >= 1 && near+size-1 <= len(content) &&
		kbFeedbackAnchor(content[near-1:near-1+size]) == anchor {
		return near, true
	}
	best, found := 0, false
	for i := 0; i+size <= len(content); i++ {
		if kbFeedbackAnchor(content[i:i+size]) != anchor {
			continue
		}
		start := i + 1
		if !found || absInt(start-near) < absInt(best-near) {
			best, found = start, true
		}
	}
	return best, found
}

// kbFeedbackAnchor hashes the referenced lines: each line trimmed, joined with
// newlines, the whole trimmed. Blank text has no anchor.
func kbFeedbackAnchor(lines []string) string {
	parts := make([]string, len(lines))
	for i, l := range lines {
		parts[i] = strings.TrimSpace(l)
	}
	text := strings.TrimSpace(strings.Join(parts, "\n"))
	if text == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(text))
	return "sha256:" + hex.EncodeToString(sum[:])[:16]
}

// kbFeedbackID derives a short, stable id for a note, unique within the page.
func kbFeedbackID(n KbFeedbackNote, used map[string]bool) string {
	seed := n.Anchor + "\n" + n.Note + "\n" + n.Author + "\n" + n.Created.String()
	for i := 0; ; i++ {
		input := seed
		if i > 0 {
			input += "\n" + strconv.Itoa(i)
		}
		sum := sha256.Sum256([]byte(input))
		id := "fb-" + hex.EncodeToString(sum[:])[:8]
		if !used[id] {
			return id
		}
	}
}

// kbFeedbackAuthor is the name a note is shown under.
func kbFeedbackAuthor(d KbFeedbackDraft) string {
	if name := oneLine(d.AuthorName); name != "" {
		return name
	}
	if strings.TrimSpace(d.Author) != "" {
		return SanitizeHandle(d.Author)
	}
	return commentAuthorFallback
}

// renderKbFeedbackEntry renders one new note.
func renderKbFeedbackEntry(n KbFeedbackNote, source []string, quote string) []string {
	out := []string{
		renderKbFeedbackMarker(n),
		"### " + escapeKbFeedbackText(n.Author) + " feedback: " + n.ID,
		"",
		"> " + kbFeedbackLinesLabel(n.StartLine, n.EndLine) + ":",
		">",
	}
	for _, line := range source {
		if strings.TrimSpace(line) == "" {
			out = append(out, ">")
			continue
		}
		out = append(out, "> "+line)
	}
	out = append(out, "")
	if q := oneLine(quote); q != "" {
		out = append(out, "Selected: “"+escapeKbFeedbackText(q)+"”", "")
	}
	return append(out, strings.Split(escapeKbFeedbackText(n.Note), "\n")...)
}

// renderKbFeedbackMarker renders the HTML comment that carries a note's
// metadata.
func renderKbFeedbackMarker(n KbFeedbackNote) string {
	attrs := []string{
		kbFeedbackAttr("id", n.ID),
		kbFeedbackAttr("anchor", n.Anchor),
		kbFeedbackAttr("lines", strconv.Itoa(n.StartLine)+"-"+strconv.Itoa(n.EndLine)),
		kbFeedbackAttr("author", n.Author),
	}
	if n.Email != "" {
		attrs = append(attrs, kbFeedbackAttr("email", n.Email))
	}
	if !n.Created.IsZero() {
		attrs = append(attrs, kbFeedbackAttr("created", n.Created.String()))
	}
	return kbFeedbackNotePrefix + strings.Join(attrs, " ") + " " + kbFeedbackNoteSuffix
}

func kbFeedbackAttr(key, value string) string {
	return key + `="` + html.EscapeString(value) + `"`
}

// parseKbFeedbackMarker reads a note's metadata back. It reports false when the
// marker cannot be anchored, which makes the note prunable.
func parseKbFeedbackMarker(line string) (KbFeedbackNote, bool) {
	attrs := map[string]string{}
	for _, m := range kbFeedbackAttrRE.FindAllStringSubmatch(line, -1) {
		attrs[m[1]] = html.UnescapeString(m[2])
	}
	n := KbFeedbackNote{
		ID:     attrs["id"],
		Anchor: attrs["anchor"],
		Author: attrs["author"],
		Email:  attrs["email"],
	}
	if ts, err := ParseTimestamp(attrs["created"]); err == nil {
		n.Created = ts
	}
	start, end, ok := parseKbFeedbackLines(attrs["lines"])
	n.StartLine, n.EndLine = start, end
	return n, ok && n.ID != "" && n.Anchor != ""
}

func parseKbFeedbackLines(s string) (start, end int, ok bool) {
	first, second, found := strings.Cut(strings.TrimSpace(s), "-")
	start, err := strconv.Atoi(first)
	if err != nil || start < 1 {
		return 0, 0, false
	}
	if !found {
		return start, start, true
	}
	end, err = strconv.Atoi(second)
	if err != nil || end < start {
		return 0, 0, false
	}
	return start, end, true
}

// kbFeedbackNoteText recovers the note from an entry: whatever follows the
// heading, the quoted source and the "Selected:" line.
func kbFeedbackNoteText(lines []string) string {
	i := 1
	for i < len(lines) {
		t := strings.TrimSpace(lines[i])
		if t == "" || strings.HasPrefix(t, "### ") || strings.HasPrefix(t, ">") ||
			strings.HasPrefix(t, "Selected: ") {
			i++
			continue
		}
		break
	}
	if i >= len(lines) {
		return ""
	}
	return unescapeKbFeedbackText(strings.TrimSpace(strings.Join(lines[i:], "\n")))
}

func kbFeedbackLinesLabel(start, end int) string {
	if start == end {
		return "Line " + strconv.Itoa(start)
	}
	return "Lines " + strconv.Itoa(start) + "–" + strconv.Itoa(end)
}

// escapeKbFeedbackText keeps free text from opening or closing an HTML comment,
// which is what would let a note forge a marker of the block.
func escapeKbFeedbackText(s string) string {
	s = strings.ReplaceAll(s, "<!--", "&lt;!--")
	return strings.ReplaceAll(s, "-->", "--&gt;")
}

func unescapeKbFeedbackText(s string) string {
	s = strings.ReplaceAll(s, "&lt;!--", "<!--")
	return strings.ReplaceAll(s, "--&gt;", "-->")
}

// oneLine collapses whitespace, newlines included, into single spaces.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func trimTrailingBlank(lines []string) []string {
	n := len(lines)
	for n > 0 && strings.TrimSpace(lines[n-1]) == "" {
		n--
	}
	return lines[:n]
}

func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
