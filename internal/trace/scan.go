package trace

import (
	"bytes"
	"fmt"
	"go/scanner"
	"go/token"
	"sort"
	"strings"

	"github.com/digiogithub/git-in-track/internal/core"
)

// Kind says what a marker adds to a requirement's trace (R-MARK-3).
type Kind string

// The two marker kinds.
const (
	KindImplements Kind = "implements" // code that realizes the requirement
	KindVerifies   Kind = "verifies"   // a test that verifies it
)

// Diagnostic codes of the marker scan (docs/03 section 21.7).
const (
	CodeMarkerSyntax   core.Code = "W-MARKER-SYNTAX"
	CodeMarkerDangling core.Code = "W-MARKER-DANGLING"
)

// Marker is one requirement ref found in a marker line. A line naming three
// refs yields three markers with the same path and line.
type Marker struct {
	Path    string              `json:"path"`              // repository-relative, "/"-separated
	Line    int                 `json:"line"`              // 1-based
	Kind    Kind                `json:"kind"`              // implements or verifies
	Project core.ProjectKey     `json:"project,omitempty"` // the "<KEY>/" qualifier, when written
	Ref     core.RequirementRef `json:"ref"`
	Symbol  string              `json:"symbol,omitempty"` // enclosing symbol; empty means the whole file
}

// Target renders the ref as written: "WEB/WEB-SP-0001.R1" or "GIT-SP-0003.R2".
func (m Marker) Target() string {
	if m.Project != "" {
		return string(m.Project) + "/" + m.Ref.String()
	}
	return m.Ref.String()
}

// TraceRef renders the location in the trace-ref form of ADR-037 section 4:
// "<repo-path>" or "<repo-path>#<symbol>".
func (m Marker) TraceRef() string {
	if m.Symbol == "" {
		return m.Path
	}
	return m.Path + "#" + m.Symbol
}

// Finding is a diagnostic of the marker scan.
type Finding struct {
	Code    core.Code `json:"code"`
	Path    string    `json:"path"`
	Line    int       `json:"line"`
	Message string    `json:"message"`
}

// FileResult is what one file contributes to the scan.
type FileResult struct {
	Markers  []Marker  `json:"markers,omitempty"`
	Findings []Finding `json:"findings,omitempty"`
}

func (r FileResult) empty() bool { return len(r.Markers) == 0 && len(r.Findings) == 0 }

// ScanFile extracts the markers of one file. p is the repository-relative
// path, used both for the result and to pick the file type; a path outside
// the file-type table yields an empty result. ScanFile never touches the
// filesystem.
func ScanFile(p string, src []byte) FileResult {
	ft, ok := typeOf(p)
	if !ok || !mayHoldMarker(src) {
		return FileResult{}
	}
	if ft.lang == langGo {
		return scanGo(p, src)
	}
	return scanLines(p, src, ft)
}

// mayHoldMarker is the cheap prefilter that keeps a full scan of a large
// repository fast: a file naming neither keyword cannot hold a marker.
func mayHoldMarker(src []byte) bool {
	return bytes.Contains(src, []byte(kwImplements+":")) || bytes.Contains(src, []byte(kwVerifies+":"))
}

// hit is a marker line before symbol attribution.
type hit struct {
	line int
	kind Kind
	refs []markerRef
}

// collector accumulates the markers and findings of one file.
type collector struct {
	path string
	res  FileResult
}

func (c *collector) add(h hit, symbol string) {
	for _, r := range h.refs {
		c.res.Markers = append(c.res.Markers, Marker{
			Path: c.path, Line: h.line, Kind: h.kind, Project: r.Project, Ref: r.Ref, Symbol: symbol,
		})
	}
}

func (c *collector) malformed(line int, kind Kind) {
	kw := kwImplements
	if kind == KindVerifies {
		kw = kwVerifies
	}
	c.res.Findings = append(c.res.Findings, Finding{
		Code: CodeMarkerSyntax, Path: c.path, Line: line,
		Message: fmt.Sprintf("malformed %s: marker: want one or more comma-separated <SPEC-ID>.R<n> refs and nothing else", kw),
	})
}

// splitLines splits a source into lines without their terminators.
func splitLines(src []byte) []string {
	lines := strings.Split(string(src), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSuffix(l, "\r")
	}
	return lines
}

// scanLines is the line rule of every file type but Go.
func scanLines(p string, src []byte, ft fileType) FileResult {
	c := &collector{path: p}
	lines := splitLines(src)
	sym := newSymbolizer(ft.lang)
	var pending []hit
	var pendingSym string
	flush := func(symbol string) {
		for _, h := range pending {
			c.add(h, symbol)
		}
		pending = pending[:0]
	}
	var fence fenceState
	open := blockNone
	for i, line := range lines {
		lineNo := i + 1
		if ft.markdown && open == blockNone && fence.step(line) {
			continue
		}
		if sym.inLiteral() {
			// A line of a template literal is code, never a comment.
			flush(pendingSym)
			sym.step(line)
			continue
		}
		kind, refs, res := matchLine(line, ft.syntax, open)
		comment := isCommentLine(line, ft.syntax, open)
		open = nextBlockState(line, ft.syntax, open)
		switch res {
		case lineMalformed:
			c.malformed(lineNo, kind)
		case lineMarker:
			if len(pending) == 0 {
				pendingSym = sym.enclosing(line)
			}
			pending = append(pending, hit{line: lineNo, kind: kind, refs: refs})
			continue
		}
		if comment || sym.transparent(line) {
			continue
		}
		if strings.TrimSpace(line) == "" {
			flush(pendingSym)
			continue
		}
		if decl, ok := sym.step(line); ok {
			flush(decl)
			continue
		}
		flush(pendingSym)
	}
	flush(pendingSym)
	return c.res
}

// fenceState tracks Markdown fenced code blocks, inside which nothing is a
// marker (R-MARK-1).
type fenceState struct {
	char byte
	size int
}

// step reports whether the line is part of a fence, the fence lines included.
func (f *fenceState) step(line string) bool {
	s := strings.TrimLeft(line, " ")
	if len(line)-len(s) > 3 {
		return f.size > 0
	}
	n := 0
	for n < len(s) && (s[n] == '`' || s[n] == '~') && s[n] == s[0] {
		n++
	}
	if f.size > 0 {
		if n >= f.size && s[0] == f.char && strings.TrimSpace(s[n:]) == "" {
			f.size = 0
		}
		return true
	}
	if n >= 3 {
		f.char, f.size = s[0], n
		return true
	}
	return false
}

// scanGo scans a Go file with the Go lexer, so that a marker inside a string
// literal is never a marker, and attributes symbols with go/parser.
func scanGo(p string, src []byte) FileResult {
	c := &collector{path: p}
	lines := splitLines(src)
	fset := token.NewFileSet()
	file := fset.AddFile(p, -1, len(src))
	var s scanner.Scanner
	s.Init(file, src, func(token.Position, string) {}, scanner.ScanComments)
	var hits []hit
	for {
		pos, tok, lit := s.Scan()
		if tok == token.EOF {
			break
		}
		if tok != token.COMMENT {
			continue
		}
		start := file.Position(pos)
		n := strings.Count(lit, "\n")
		for k := 0; k <= n; k++ {
			lineNo := start.Line + k
			if lineNo > len(lines) {
				break
			}
			line := lines[lineNo-1]
			open := blockNone
			if k == 0 {
				// The opener must be the first non-whitespace text of the line.
				if strings.TrimSpace(line[:start.Column-1]) != "" {
					continue
				}
			} else {
				open = blockC
			}
			kind, refs, res := matchLine(line, synSlash, open)
			switch res {
			case lineMalformed:
				c.malformed(lineNo, kind)
			case lineMarker:
				hits = append(hits, hit{line: lineNo, kind: kind, refs: refs})
			}
		}
	}
	if len(hits) == 0 {
		return c.res
	}
	decls := goDecls(p, src)
	for _, h := range hits {
		c.add(h, decls.symbolAt(h.line))
	}
	sort.SliceStable(c.res.Findings, func(i, j int) bool { return c.res.Findings[i].Line < c.res.Findings[j].Line })
	return c.res
}
