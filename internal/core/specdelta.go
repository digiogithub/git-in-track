package core

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// This file is the Spec Delta parser of ADR-037 section 9 (docs/03 section
// 21.8): the "## Spec Delta" section in which a story or a task proposes
// changes to living specs. It only reads the proposal; applying it when the
// story reaches a done-category status is a vault write of its own. Like the
// block parser it is pure text processing, so it compiles to WebAssembly.

// The diagnostic codes of the Spec Delta (docs/03 section 16).
const (
	// CodeDeltaOp is a level-3 heading of a Spec Delta whose operation is not
	// ADDED, MODIFIED or REMOVED, or that is otherwise malformed.
	CodeDeltaOp Code = "E-DELTA-OP"
	// CodeDeltaTarget is an operation naming the wrong kind of target: ADDED a
	// requirement, MODIFIED or REMOVED a spec, or a Supersedes: line that is
	// not a requirement ref.
	CodeDeltaTarget Code = "E-DELTA-TARGET"
	// CodeDeltaReason is a REMOVED operation without a Reason: line.
	CodeDeltaReason Code = "E-DELTA-REASON"
	// CodeWarnDeltaDangling is an operation naming a spec or a block the index
	// does not hold. A warning: the target may arrive in a later merge.
	CodeWarnDeltaDangling Code = "W-DELTA-DANGLING"
)

// DeltaOp is the operation of one Spec Delta entry.
type DeltaOp string

// The three Spec Delta operations; the words are written in upper case.
const (
	DeltaAdded    DeltaOp = "ADDED"
	DeltaModified DeltaOp = "MODIFIED"
	DeltaRemoved  DeltaOp = "REMOVED"
)

// Valid reports whether the operation is one of the three.
func (o DeltaOp) Valid() bool { return o == DeltaAdded || o == DeltaModified || o == DeltaRemoved }

// specDeltaHeading is the level-2 heading that opens the section.
const specDeltaHeading = "Spec Delta"

// DeltaOperation is one "### <OP> <target> — <title>" entry of a Spec Delta,
// with everything up to the next level 1–3 heading (the block extent of
// R-REQ-3).
type DeltaOperation struct {
	Op DeltaOp `json:"op"`
	// Spec is the spec the operation changes.
	Spec ItemID `json:"spec"`
	// Ref is the requirement a MODIFIED or REMOVED operation names. On an
	// ADDED operation it is set only once the delta was applied and the
	// heading rewritten to carry the allocated number (R-DELTA-1).
	Ref *RequirementRef `json:"ref,omitempty"`
	// Title is the heading text after the separator; Separator is the
	// separator as written.
	Title     string `json:"title"`
	Separator string `json:"separator"`
	// Supersedes is the requirement an ADDED block replaces, read from a
	// "Supersedes: <REQREF>" line directly under the heading: the move of
	// R-REQ-6.
	Supersedes *RequirementRef `json:"supersedes,omitempty"`
	// Reason is the text of the Reason: line of a REMOVED operation.
	Reason string `json:"reason,omitempty"`
	// Statement and Scenarios are the proposed block of an ADDED or a
	// MODIFIED operation, read exactly as a spec block is read.
	Statement string     `json:"statement,omitempty"`
	Scenarios []Scenario `json:"scenarios,omitempty"`
	// Start and End are the byte range of the operation in the body, Line the
	// 1-based line of its heading and Text the extent as written.
	Start int    `json:"start"`
	End   int    `json:"end"`
	Line  int    `json:"line"`
	Text  string `json:"text"`

	// lintText is Text with the Supersedes: line blanked, so the linter reads
	// the statement that follows it; line numbers are kept.
	lintText string
}

// Target renders what the heading names: the ref when there is one, the spec
// otherwise.
func (o DeltaOperation) Target() string {
	if o.Ref != nil {
		return o.Ref.String()
	}
	return string(o.Spec)
}

// SpecDelta is the parsed "## Spec Delta" section of a body.
type SpecDelta struct {
	Operations []DeltaOperation `json:"operations"`
	Findings   []SpecFinding    `json:"findings,omitempty"`
}

// Refs returns every requirement ref the delta names — the targets of
// MODIFIED and REMOVED, the number of an applied ADDED and every Supersedes:
// line — in the order they appear. They keep their numbers reserved in the
// spec (R-REQ-5).
func (d SpecDelta) Refs() []RequirementRef {
	var out []RequirementRef
	for _, op := range d.Operations {
		if op.Ref != nil {
			out = append(out, *op.Ref)
		}
		if op.Supersedes != nil {
			out = append(out, *op.Supersedes)
		}
	}
	return out
}

// carriesSpecDelta reports whether a type proposes spec changes: stories and
// tasks do (ADR-037 section 9); in any other item the section is prose.
func carriesSpecDelta(t ItemType) bool { return t == TypeStory || t == TypeTask }

// mayHaveSpecDelta is a cheap pre-check that skips the line scan for the
// bodies, the vast majority, that have no Spec Delta section at all.
func mayHaveSpecDelta(body string) bool {
	return strings.Contains(strings.ToLower(body), "spec delta")
}

// ParseSpecDelta reads the "## Spec Delta" section of a story or task body
// (docs/03 section 21.8). Every level-3 heading inside it is an operation;
// level-4 headings (scenarios) belong to the operation above them, and a level
// 1 or 2 heading ends the section. Headings inside fenced code blocks are text.
//
// It reports the findings a body alone decides: E-DELTA-OP, E-DELTA-TARGET for
// MODIFIED or REMOVED naming a spec, E-DELTA-REASON, E-ID-GRAMMAR for a
// misspelled ref and W-REQ-SEPARATOR. An ADDED heading that carries a number
// is returned with Ref set: whether that is the applied form or a mistake
// depends on the item's status, which SpecDeltaDiagnostics decides. Unknown
// targets need the index (W-DELTA-DANGLING).
func ParseSpecDelta(body string) SpecDelta {
	out := SpecDelta{Operations: []DeltaOperation{}}
	if !mayHaveSpecDelta(body) {
		return out
	}
	body = strings.ReplaceAll(body, "\r\n", "\n")
	lines := splitSpecLines(body)

	var fence fenceTracker
	inDelta := false
	open := -1
	var openLines []bodyLine

	closeOp := func(end int) {
		if open < 0 {
			return
		}
		op := &out.Operations[open]
		op.End = end
		op.Text = body[op.Start:end]
		out.Findings = append(out.Findings, readDeltaContent(op, openLines[1:])...)
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
		closeOp(ln.start)
		switch level {
		case 1:
			inDelta = false
			continue
		case 2:
			inDelta = strings.EqualFold(headingText(ln.text, 2), specDeltaHeading)
			continue
		}
		if !inDelta {
			continue
		}
		op, finding, ok := parseDeltaHeading(ln.text, lineNo)
		if finding != nil {
			out.Findings = append(out.Findings, *finding)
		}
		if !ok {
			continue
		}
		op.Start, op.Line = ln.start, lineNo
		out.Operations = append(out.Operations, op)
		open = len(out.Operations) - 1
		openLines = []bodyLine{ln}
	}
	closeOp(len(body))
	return out
}

// cutField splits off the first run of non-blank characters of s.
func cutField(s string) (field, rest string) {
	s = strings.TrimLeft(s, " \t")
	if i := strings.IndexAny(s, " \t"); i >= 0 {
		return s[:i], strings.TrimLeft(s[i:], " \t")
	}
	return s, ""
}

// parseDeltaHeading reads one level-3 heading of a Spec Delta section. It
// returns the operation the heading opens, if any, and at most one finding.
func parseDeltaHeading(line string, lineNo int) (DeltaOperation, *SpecFinding, bool) {
	finding := func(code Code, sev Severity, ref, format string, args ...any) *SpecFinding {
		return &SpecFinding{Code: code, Severity: sev, Line: lineNo, Ref: ref, Message: fmt.Sprintf(format, args...)}
	}
	malformed := func(format string, args ...any) (DeltaOperation, *SpecFinding, bool) {
		return DeltaOperation{}, finding(CodeDeltaOp, SeverityError, "", format, args...), false
	}
	const want = "want \"### ADDED <SPEC-ID> — <title>\", \"### MODIFIED <REQREF> — <title>\" or \"### REMOVED <REQREF> — <title>\""

	opTok, rest := cutField(headingText(line, 3))
	op := DeltaOperation{Op: DeltaOp(opTok)}
	if !op.Op.Valid() {
		return malformed("Spec Delta heading %q has no operation: %s", line, want)
	}
	target, rest := cutField(rest)
	if target == "" {
		return malformed("Spec Delta heading %q has no target: %s", line, want)
	}

	if ref, err := ParseRequirementRef(target); err == nil {
		// On ADDED this is the applied form or a mistake, which depends on the
		// item's status: SpecDeltaDiagnostics decides.
		op.Spec, op.Ref = ref.Spec, &ref
	} else if _, code, _, err := ParseItemID(target); err == nil {
		if code != CodeSpec {
			return DeltaOperation{}, finding(CodeDeltaTarget, SeverityError, target,
				"%s %s names a %s: a Spec Delta changes a spec or a requirement", op.Op, target, typeName(code)), false
		}
		if op.Op != DeltaAdded {
			return DeltaOperation{}, finding(CodeDeltaTarget, SeverityError, target,
				"%s names the spec %s: want the requirement it changes, <SPEC-ID>.R<n>", op.Op, target), false
		}
		op.Spec = ItemID(target)
	} else if looseReqRefRE.MatchString(target) {
		return DeltaOperation{}, finding(CodeIDGrammar, SeverityError, target,
			"%q is not a requirement ref: want <KEY>-SP-<NNNN>.R<n> with an unpadded R<n>", target), false
	} else {
		return malformed("Spec Delta heading %q names %q, which is neither a spec nor a requirement: %s", line, target, want)
	}

	for _, candidate := range reqSeparators {
		if rest == candidate {
			op.Separator = candidate
			break
		}
		if after, found := strings.CutPrefix(rest, candidate+" "); found {
			op.Separator, op.Title = candidate, strings.TrimSpace(after)
			break
		}
	}
	switch {
	case op.Separator == "":
		return malformed("Spec Delta heading %q has no \" %s <title>\" after its target: %s", line, ReqSeparator, want)
	case op.Title == "":
		return malformed("Spec Delta heading %q has an empty title", line)
	case utf8.RuneCountInString(op.Title) > maxRequirementTitle:
		return malformed("the title of Spec Delta heading %q is longer than %d characters", line, maxRequirementTitle)
	}
	if op.Separator != ReqSeparator {
		return op, finding(CodeWarnReqSeparator, SeverityWarning, op.Target(),
			"Spec Delta heading of %s %s uses %q instead of %q", op.Op, op.Target(), op.Separator, ReqSeparator), true
	}
	return op, nil, true
}

// readDeltaContent fills in what follows the heading of an operation: the
// Supersedes: line and the proposed block of ADDED, the replacement block of
// MODIFIED, the Reason: line of REMOVED. It returns the findings about them.
func readDeltaContent(op *DeltaOperation, lines []bodyLine) []SpecFinding {
	var findings []SpecFinding
	op.lintText = op.Text
	switch op.Op {
	case DeltaAdded:
		for i, ln := range lines {
			trimmed := strings.TrimSpace(ln.text)
			if trimmed == "" {
				continue
			}
			value, ok := strings.CutPrefix(trimmed, "Supersedes:")
			if !ok {
				break
			}
			lineNo := op.Line + 1 + i
			value = strings.TrimSpace(value)
			if ref, err := ParseRequirementRef(value); err == nil {
				op.Supersedes = &ref
			} else {
				findings = append(findings, SpecFinding{
					Code: CodeDeltaTarget, Severity: SeverityError, Line: lineNo, Ref: op.Target(),
					Message: fmt.Sprintf("Supersedes: %q is not a requirement ref: want <SPEC-ID>.R<n>", value),
				})
			}
			// The line is metadata, not the statement: blank it for the lint
			// and the statement, keeping every line number.
			lines = append([]bodyLine(nil), lines...)
			lines[i].text = ""
			rel := ln.start - op.Start
			op.lintText = op.Text[:rel] + op.Text[rel+len(ln.text):]
			break
		}
		op.Statement, op.Scenarios = readBlockContent(lines, op.Line)
	case DeltaModified:
		op.Statement, op.Scenarios = readBlockContent(lines, op.Line)
	case DeltaRemoved:
		var fence fenceTracker
		found := false
		for _, ln := range lines {
			if fence.step(ln.text) {
				continue
			}
			if value, ok := strings.CutPrefix(strings.TrimSpace(ln.text), "Reason:"); ok {
				op.Reason, found = strings.TrimSpace(value), true
				break
			}
		}
		if !found || op.Reason == "" {
			msg := "REMOVED %s has no \"Reason:\" line: say why the requirement goes"
			if found {
				msg = "REMOVED %s has an empty \"Reason:\" line: say why the requirement goes"
			}
			findings = append(findings, SpecFinding{
				Code: CodeDeltaReason, Severity: SeverityError, Line: op.Line, Ref: op.Target(),
				Message: fmt.Sprintf(msg, op.Target()),
			})
		}
	}
	return findings
}

// SpecDeltaDiagnostics returns the findings about the Spec Delta of a story or
// a task that the item and its project decide (docs/03 section 21.8): the
// parser's findings, E-DELTA-TARGET for an ADDED heading that carries a number
// while the item is not in a done-category status — the number is allocated
// only when the delta is applied — and the LINT-REQ-* grammar lint of every
// added and replacement block, at the severity specs.lint gives each rule. A
// nil cfg skips the status check and lints at warning.
//
// Dangling targets need the whole vault and are reported by the index as
// W-DELTA-DANGLING. ValidateItem runs this before every write and the index
// runs it over every story and task, so doctor reports it.
func SpecDeltaDiagnostics(item *Item, cfg *ProjectConfig) []Diagnostic {
	if item == nil || !carriesSpecDelta(item.Type) {
		return nil
	}
	return specDeltaDiagnostics(item, ParseSpecDelta(item.Body), cfg)
}

func specDeltaDiagnostics(item *Item, delta SpecDelta, cfg *ProjectConfig) []Diagnostic {
	d := &diagSet{path: item.Path}
	for _, f := range delta.Findings {
		field := "body"
		if f.Ref != "" {
			field = "body." + f.Ref
		}
		d.add(f.Code, f.Severity, field, fmt.Sprintf("line %d of the body: %s", f.Line, f.Message))
	}
	applied := cfg != nil && cfg.CategoryOf(item.Status) == CategoryDone
	for _, op := range delta.Operations {
		field := "body." + op.Target()
		if op.Op == DeltaAdded && op.Ref != nil && cfg != nil && !applied {
			d.errorf(field, CodeDeltaTarget,
				"line %d of the body: ADDED names the requirement %s: an added block gets its number when the %s is done; write \"### ADDED %s %s <title>\"",
				op.Line, op.Ref, item.Type, op.Spec, ReqSeparator)
		}
		if op.Op == DeltaRemoved {
			continue
		}
		ref := RequirementRef{Spec: op.Spec}
		if op.Ref != nil {
			ref = *op.Ref
		}
		name := string(op.Op) + " " + op.Target()
		for _, f := range lintBlockText(ref, name, op.lintText, op.Line, cfg.SpecLint()) {
			d.add(f.Rule, f.Severity, field, fmt.Sprintf("line %d of the body: %s", f.Line, f.Message))
		}
	}
	orderDiagnostics(d.out)
	return d.out
}

// validateSpecDelta adds the Spec Delta findings of a story or a task.
func validateSpecDelta(d *diagSet, item *Item, cfg *ProjectConfig) {
	d.out = append(d.out, SpecDeltaDiagnostics(item, cfg)...)
}
