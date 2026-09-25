package core

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// This file holds the structural half of the spec layer of ADR-037 (docs/03
// section 21): requirement refs, the requirement-block parser and the two
// per-requirement hashes. It is pure text processing over a body the caller
// already read, so it compiles to WebAssembly unchanged (ADR-003).

// RequirementRef addresses one requirement block: the spec it lives in and its
// permanent number, written "<SPEC-ID>.R<n>" (R-REQ-4), e.g. GIT-SP-0003.R2.
type RequirementRef struct {
	Spec   ItemID
	Number int
}

// reqRefRE is the strict grammar of a requirement ref: an SP id, ".R" and an
// unpadded positive number. R<n> has exactly one spelling (ADR-037 section 3).
var reqRefRE = regexp.MustCompile(`^([A-Z][A-Z0-9]{1,9}-SP-\d{4,})\.R([1-9]\d*)$`)

// looseReqRefRE matches what was clearly meant as a requirement ref, including
// the spellings the strict grammar refuses (R02, r2, a lower-case key, a short
// spec number). A token that matches it but not reqRefRE is E-ID-GRAMMAR.
var looseReqRefRE = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]*-[Ss][Pp]-(\d+)\.[Rr](\d+)$`)

// reqKeyRE is the grammar of a key of the requirements: map, "R<n>".
var reqKeyRE = regexp.MustCompile(`^R([1-9]\d*)$`)

// ParseRequirementRef parses the strict "<KEY>-SP-<NNNN>.R<n>" form. A project
// qualifier ("WEB/…") is not part of a ref: it belongs to the link-target
// grammar that wraps one.
func ParseRequirementRef(s string) (RequirementRef, error) {
	m := reqRefRE.FindStringSubmatch(s)
	if m == nil {
		return RequirementRef{}, fmt.Errorf("parse requirement ref %q: want <KEY>-SP-<NNNN>.R<n>", s)
	}
	n, err := strconv.Atoi(m[2])
	if err != nil {
		return RequirementRef{}, fmt.Errorf("parse requirement ref %q: %w", s, err)
	}
	return RequirementRef{Spec: ItemID(m[1]), Number: n}, nil
}

// IsRequirementRef reports whether s matches the strict requirement-ref grammar.
func IsRequirementRef(s string) bool { return reqRefRE.MatchString(s) }

// String renders the ref, e.g. "GIT-SP-0003.R2".
func (r RequirementRef) String() string { return fmt.Sprintf("%s.R%d", r.Spec, r.Number) }

// MarshalText renders the ref in its written form, so that JSON carries
// "GIT-SP-0003.R2" rather than an object.
func (r RequirementRef) MarshalText() ([]byte, error) { return []byte(r.String()), nil }

// UnmarshalText parses the written form.
func (r *RequirementRef) UnmarshalText(data []byte) error {
	ref, err := ParseRequirementRef(string(data))
	if err != nil {
		return err
	}
	*r = ref
	return nil
}

// Key returns the key of the requirement inside its spec's requirements: map.
func (r RequirementRef) Key() string { return RequirementKey(r.Number) }

// Anchor returns the HTML/Markdown anchor of the block: the ref lower-cased
// with "." replaced by "-" (R-REQ-7), e.g. "git-sp-0003-r2".
func (r RequirementRef) Anchor() string {
	return strings.ToLower(strings.ReplaceAll(r.String(), ".", "-"))
}

// RequirementKey renders the requirements: map key of a number, "R<n>".
func RequirementKey(n int) string { return "R" + strconv.Itoa(n) }

// ParseRequirementKey returns the number of a well-formed "R<n>" key.
func ParseRequirementKey(key string) (int, bool) {
	m := reqKeyRE.FindStringSubmatch(key)
	if m == nil {
		return 0, false
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return 0, false
	}
	return n, true
}

// looseRequirementNumber reads the number out of a key a human may have
// misspelled ("R02", "r2"). It is only used to keep such a number reserved,
// never to address anything.
func looseRequirementNumber(key string) (int, bool) {
	if len(key) < 2 || (key[0] != 'R' && key[0] != 'r') {
		return 0, false
	}
	n, err := strconv.Atoi(key[1:])
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

// The requirement-heading separators. The em dash is canonical and the only one
// a writer emits; the others are accepted on read as W-REQ-SEPARATOR (R-REQ-1).
const (
	ReqSeparator = "—" // U+2014 EM DASH
	reqSepEnDash = "–" // U+2013 EN DASH
)

// reqSeparators lists every accepted separator, the canonical one first and
// "--" before "-" so that the longer spelling wins.
var reqSeparators = []string{ReqSeparator, reqSepEnDash, "--", "-"}

// maxRequirementTitle is the longest requirement title, in characters.
const maxRequirementTitle = 200

// RequirementBlock is one requirement of a spec body: a "### <REQREF> — <title>"
// heading and everything up to the next level 1–3 heading (R-REQ-3).
type RequirementBlock struct {
	Ref RequirementRef `json:"ref"`
	// Title is the heading text after the separator.
	Title string `json:"title"`
	// Separator is the separator as written; anything but ReqSeparator is
	// W-REQ-SEPARATOR.
	Separator string `json:"separator"`
	// Statement is the first paragraph after the heading (R-REQ-2), without
	// its surrounding blank lines. It may be empty.
	Statement string `json:"statement"`
	// Scenarios are the "#### Scenario:" sub-blocks, in order.
	Scenarios []Scenario `json:"scenarios,omitempty"`
	// Start and End are the byte range of the block extent in the body the
	// block was parsed from (Item.Body): body[Start:End] is Text.
	Start int `json:"start"`
	End   int `json:"end"`
	// Line is the 1-based line of the heading inside that body.
	Line int `json:"line"`
	// Text is the block extent exactly as written, trailing blank lines
	// included.
	Text string `json:"text"`
	// Rev is the block rev of R-REQ-REV-1: the fingerprint of what the
	// requirement says, the value verified.rev records.
	Rev Rev `json:"rev"`
}

// Scenario is one "#### Scenario: <name>" sub-block of a requirement. Its steps
// are the list items that follow it, verbatim after the bullet; interpreting
// the WHEN/THEN keywords is the grammar lint's job, never the parser's.
type Scenario struct {
	Name  string   `json:"name"`
	Line  int      `json:"line"`
	Steps []string `json:"steps,omitempty"`
}

// SpecFinding is a structural problem the block parser found in a spec body. It
// becomes a Diagnostic once the validator knows which file it came from.
type SpecFinding struct {
	Code     Code     `json:"code"`
	Severity Severity `json:"severity"`
	// Line is the 1-based line inside the body.
	Line    int    `json:"line"`
	Ref     string `json:"ref,omitempty"`
	Message string `json:"message"`
}

// SpecBody is the result of parsing the body of a spec.
type SpecBody struct {
	Blocks   []RequirementBlock `json:"blocks"`
	Findings []SpecFinding      `json:"findings,omitempty"`
}

// Block returns the first block that claims a number. A duplicated number is
// E-REQ-DUPLICATE and reported by the validator; lookups take the first one.
func (b SpecBody) Block(n int) (RequirementBlock, bool) {
	for _, blk := range b.Blocks {
		if blk.Ref.Number == n {
			return blk, true
		}
	}
	return RequirementBlock{}, false
}

// bodyLine is one line of a body with its byte offset.
type bodyLine struct {
	text  string
	start int // offset of the first byte of the line
	end   int // offset just past its "\n", or the end of the body
}

// splitSpecLines cuts a body into lines, keeping the offsets.
func splitSpecLines(body string) []bodyLine {
	var out []bodyLine
	for offset := 0; offset < len(body); {
		i := strings.IndexByte(body[offset:], '\n')
		if i < 0 {
			out = append(out, bodyLine{text: body[offset:], start: offset, end: len(body)})
			break
		}
		out = append(out, bodyLine{text: body[offset : offset+i], start: offset, end: offset + i + 1})
		offset += i + 1
	}
	return out
}

// fenceTracker follows fenced code blocks (``` and ~~~, CommonMark style), so
// that a heading inside one is never a block boundary (R-REQ-3).
type fenceTracker struct {
	char byte
	size int
}

// inside reports whether the tracker is inside a fence.
func (f *fenceTracker) inside() bool { return f.size > 0 }

// step feeds one line and reports whether that line is part of a fence,
// including the opening and closing fence lines themselves.
func (f *fenceTracker) step(line string) bool {
	trimmed := strings.TrimLeft(line, " ")
	if len(line)-len(trimmed) > 3 || trimmed == "" {
		return f.inside()
	}
	c := trimmed[0]
	if c != '`' && c != '~' {
		return f.inside()
	}
	n := 0
	for n < len(trimmed) && trimmed[n] == c {
		n++
	}
	if n < 3 {
		return f.inside()
	}
	if !f.inside() {
		if c == '`' && strings.ContainsRune(trimmed[n:], '`') {
			// A backtick fence's info string cannot contain a backtick.
			return false
		}
		f.char, f.size = c, n
		return true
	}
	if c == f.char && n >= f.size && strings.TrimSpace(trimmed[n:]) == "" {
		f.char, f.size = 0, 0
	}
	return true
}

// atxLevel returns the level of an ATX heading written at column 0 — one to six
// "#" followed by a space, a tab or the end of the line — or zero.
func atxLevel(line string) int {
	n := 0
	for n < len(line) && line[n] == '#' {
		n++
	}
	if n == 0 || n > 6 {
		return 0
	}
	if n == len(line) || line[n] == ' ' || line[n] == '\t' {
		return n
	}
	return 0
}

// headingText returns the text of an ATX heading of the given level.
func headingText(line string, level int) string {
	return strings.TrimSpace(line[level:])
}

// ParseSpecBody extracts the requirement blocks of a spec body (docs/03 section
// 21.2) and reports the structural findings: E-ID-GRAMMAR for a misspelled ref,
// E-REQ-FOREIGN for a ref naming another spec, W-REQ-SEPARATOR for a non-canonical
// separator and W-REQ-HEADING for a level-3 heading under "## Requirements" that
// is not a requirement heading. E-REQ-DUPLICATE, which needs every block, is left
// to the validator.
//
// A block whose statement or scenarios break the EARS grammar is still a block:
// the grammar is linted, never a parse gate (R-REQ-2).
func ParseSpecBody(spec ItemID, body string) SpecBody {
	body = strings.ReplaceAll(body, "\r\n", "\n")
	lines := splitSpecLines(body)
	out := SpecBody{Blocks: []RequirementBlock{}}

	var fence fenceTracker
	inRequirements := false
	open := -1 // index into out.Blocks of the block being read, or -1
	var openLines []bodyLine

	closeBlock := func(end int) {
		if open < 0 {
			return
		}
		blk := &out.Blocks[open]
		blk.End = end
		blk.Text = body[blk.Start:end]
		blk.Rev = BlockRev(blk.Text)
		blk.Statement, blk.Scenarios = readBlockContent(openLines[1:], blk.Line)
		open, openLines = -1, nil
	}

	for i, ln := range lines {
		lineNo := i + 1
		if fence.step(ln.text) {
			if open >= 0 {
				openLines = append(openLines, ln)
			}
			continue
		}
		level := atxLevel(ln.text)
		if level < 1 || level > 3 {
			if open >= 0 {
				openLines = append(openLines, ln)
			}
			continue
		}
		closeBlock(ln.start)
		switch level {
		case 1:
			inRequirements = false
			continue
		case 2:
			inRequirements = strings.EqualFold(headingText(ln.text, 2), "Requirements")
			continue
		}

		blk, finding, ok := parseRequirementHeading(spec, ln.text, lineNo, inRequirements)
		if finding != nil {
			out.Findings = append(out.Findings, *finding)
		}
		if !ok {
			continue
		}
		blk.Start = ln.start
		blk.Line = lineNo
		out.Blocks = append(out.Blocks, blk)
		open = len(out.Blocks) - 1
		openLines = []bodyLine{ln}
	}
	closeBlock(len(body))
	return out
}

// parseRequirementHeading reads one level-3 heading. It returns the block the
// heading opens, if any, and at most one finding about it.
func parseRequirementHeading(spec ItemID, line string, lineNo int, inRequirements bool) (RequirementBlock, *SpecFinding, bool) {
	notARequirement := func(format string, args ...any) (RequirementBlock, *SpecFinding, bool) {
		if !inRequirements {
			return RequirementBlock{}, nil, false
		}
		return RequirementBlock{}, &SpecFinding{
			Code: CodeWarnReqHeading, Severity: SeverityWarning, Line: lineNo,
			Message: fmt.Sprintf(format, args...),
		}, false
	}

	if !strings.HasPrefix(line, "### ") {
		return notARequirement("level-3 heading %q is not a requirement heading: want \"### <REQREF> %s <title>\"", line, ReqSeparator)
	}
	text := strings.TrimRight(line[len("### "):], " \t")
	token, rest, _ := strings.Cut(text, " ")

	ref, err := ParseRequirementRef(token)
	if err != nil {
		if looseReqRefRE.MatchString(token) {
			return RequirementBlock{}, &SpecFinding{
				Code: CodeIDGrammar, Severity: SeverityError, Line: lineNo, Ref: token,
				Message: fmt.Sprintf("%q is not a requirement ref: want <KEY>-SP-<NNNN>.R<n> with an unpadded R<n>", token),
			}, false
		}
		return notARequirement("level-3 heading %q is not a requirement heading: want \"### <REQREF> %s <title>\"", line, ReqSeparator)
	}
	if ref.Spec != spec {
		return RequirementBlock{}, &SpecFinding{
			Code: CodeReqForeign, Severity: SeverityError, Line: lineNo, Ref: token,
			Message: fmt.Sprintf("requirement heading names %s, which belongs to %s, not to %s", token, ref.Spec, spec),
		}, false
	}

	sep, title := "", ""
	for _, candidate := range reqSeparators {
		if after, found := strings.CutPrefix(rest, candidate+" "); found {
			sep, title = candidate, strings.TrimSpace(after)
			break
		}
	}
	switch {
	case sep == "":
		return RequirementBlock{}, &SpecFinding{
			Code: CodeWarnReqHeading, Severity: SeverityWarning, Line: lineNo, Ref: token,
			Message: fmt.Sprintf("heading of %s has no \" %s <title>\": it is prose, not a requirement", token, ReqSeparator),
		}, false
	case title == "":
		return RequirementBlock{}, &SpecFinding{
			Code: CodeWarnReqHeading, Severity: SeverityWarning, Line: lineNo, Ref: token,
			Message: fmt.Sprintf("heading of %s has an empty title: it is prose, not a requirement", token),
		}, false
	case utf8.RuneCountInString(title) > maxRequirementTitle:
		return RequirementBlock{}, &SpecFinding{
			Code: CodeWarnReqHeading, Severity: SeverityWarning, Line: lineNo, Ref: token,
			Message: fmt.Sprintf("title of %s is longer than %d characters: it is prose, not a requirement", token, maxRequirementTitle),
		}, false
	}

	blk := RequirementBlock{Ref: ref, Title: title, Separator: sep}
	var finding *SpecFinding
	if sep != ReqSeparator {
		finding = &SpecFinding{
			Code: CodeWarnReqSeparator, Severity: SeverityWarning, Line: lineNo, Ref: ref.String(),
			Message: fmt.Sprintf("heading of %s uses %q instead of %q", ref, sep, ReqSeparator),
		}
	}
	return blk, finding, true
}

// scenarioPrefix opens a scenario sub-block (R-REQ-2).
const scenarioPrefix = "#### Scenario:"

// readBlockContent extracts the statement and the scenarios of a block from its
// lines after the heading. headingLine is the body line of the heading, so that
// scenario lines are reported in the same numbering.
func readBlockContent(lines []bodyLine, headingLine int) (string, []Scenario) {
	var fence fenceTracker
	var statement []string
	statementDone := false
	var scenarios []Scenario
	current := -1

	for i, ln := range lines {
		lineNo := headingLine + 1 + i
		inFence := fence.step(ln.text)
		trimmed := strings.TrimSpace(ln.text)

		if !statementDone {
			switch {
			case inFence:
				// A fence ends the statement; one before any prose means the
				// block has none.
				statementDone = true
			case trimmed == "" && len(statement) == 0:
				continue
			case trimmed == "" || atxLevel(ln.text) > 0:
				statementDone = true
			default:
				statement = append(statement, trimmed)
				continue
			}
		}
		if inFence {
			continue
		}
		if atxLevel(ln.text) >= 4 {
			current = -1
			if name, ok := strings.CutPrefix(ln.text, scenarioPrefix); ok {
				scenarios = append(scenarios, Scenario{Name: strings.TrimSpace(name), Line: lineNo})
				current = len(scenarios) - 1
			}
			continue
		}
		if current >= 0 {
			if step, ok := listItemText(ln.text); ok {
				scenarios[current].Steps = append(scenarios[current].Steps, step)
			}
		}
	}
	return strings.Join(statement, "\n"), scenarios
}

// listItemText returns the text of a bullet list item ("- ", "* " or "+ ").
func listItemText(line string) (string, bool) {
	trimmed := strings.TrimLeft(line, " \t")
	if len(trimmed) < 2 {
		return "", false
	}
	switch trimmed[0] {
	case '-', '*', '+':
		if trimmed[1] == ' ' || trimmed[1] == '\t' {
			return strings.TrimSpace(trimmed[2:]), true
		}
	}
	return "", false
}

// canonicalBlock returns the bytes a block rev is computed over: the block
// extent with its trailing blank lines removed and exactly one "\n" appended
// (R-REQ-REV-1).
func canonicalBlock(text string) []byte {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	lines := strings.Split(text, "\n")
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		return nil
	}
	return []byte(strings.Join(lines, "\n") + "\n")
}

// BlockRev returns the block rev of a requirement's text (R-REQ-REV-1). It
// covers the heading, the statement and the scenarios and nothing else, so the
// blank lines between blocks, edits to other blocks and every front-matter
// change leave it unchanged.
func BlockRev(text string) Rev {
	return hashRev(canonicalBlock(text))
}

// RequirementRev returns the write token of one requirement (R-REQ-REV-2): the
// block bytes followed by the canonical JSON of its requirements: entry, "{}"
// when there is none. Writing verified or changing the status changes it, but
// never the block rev the stamp records. A requirement whose block is missing
// hashes its entry alone.
func RequirementRev(blockText string, entry *Requirement) (Rev, error) {
	data, err := entry.CanonicalJSON()
	if err != nil {
		return "", err
	}
	buf := append(canonicalBlock(blockText), data...)
	return hashRev(buf), nil
}

// RequirementNumbers returns every number the spec holds reserved in its own
// file: block headings, including the misspelled ones, and requirements: keys,
// including malformed ones. It is the "within the file" half of R-REQ-5.
func RequirementNumbers(spec *Item) []int {
	if spec == nil {
		return nil
	}
	seen := map[int]bool{}
	for _, blk := range ParseSpecBody(spec.ID, spec.Body).Blocks {
		seen[blk.Ref.Number] = true
	}
	for _, ln := range splitSpecLines(spec.Body) {
		// A heading whose ref is misspelled or whose separator is missing is
		// not a block, but its number is still taken.
		text, ok := strings.CutPrefix(ln.text, "### ")
		if !ok {
			continue
		}
		token, _, _ := strings.Cut(strings.TrimSpace(text), " ")
		if m := looseReqRefRE.FindStringSubmatch(token); m != nil {
			if spec.ID == "" || strings.EqualFold(strings.SplitN(token, ".", 2)[0], string(spec.ID)) {
				if n, err := strconv.Atoi(m[2]); err == nil && n > 0 {
					seen[n] = true
				}
			}
		}
	}
	for key := range spec.Requirements {
		if n, ok := looseRequirementNumber(key); ok {
			seen[n] = true
		}
	}
	out := make([]int, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	sort.Ints(out)
	return out
}
