package core

// The answer types of the requirement trace graph (ADR-037 sections 4, 5 and
// 8; docs/03 section 21.7). The graph itself is built by the native package
// internal/trace, because it needs the marker scanner and the working tree;
// these are only the shapes a host hands back through the vault seam, so
// they live here, free of any filesystem access, and compile to WebAssembly.
// Nothing here is ever written into a file: the graph is derived data.

// TraceRole says which half of a requirement's trace an edge belongs to: the
// code that realizes it or the tests that verify it.
type TraceRole string

// The two roles, named after the requirements.R<n>.trace keys.
const (
	TraceRoleCode TraceRole = "code"  // an Implements: marker or a trace.code entry
	TraceRoleTest TraceRole = "tests" // a Verifies: marker or a trace.tests entry
)

// TraceSource says where an edge of the trace graph was read from.
type TraceSource string

// The two sources. Markers and trace: entries are unioned (R-MARK-3): an edge
// both declare carries both sources, neither overrides the other.
const (
	TraceSourceMarker TraceSource = "marker" // an in-code Implements:/Verifies: marker
	TraceSourceEntry  TraceSource = "trace"  // a requirements.R<n>.trace entry
)

// CodeWarnTraceBroken is a trace: entry whose path or symbol no longer exists
// in the working tree.
const CodeWarnTraceBroken Code = "W-TRACE-BROKEN"

// TraceEdge ties one requirement to one location of code or tests.
type TraceEdge struct {
	Ref  RequirementRef `json:"ref"`
	Role TraceRole      `json:"role"`
	// Path is repository-relative and "/"-separated.
	Path string `json:"path"`
	// Symbol is the trace-ref symbol (Func, Type.Method, TestX/sub_case,
	// "describe > it"); empty means the whole file.
	Symbol  string        `json:"symbol,omitempty"`
	Sources []TraceSource `json:"sources"`
	// Lines are the 1-based lines of the markers behind the edge, sorted.
	Lines []int `json:"lines,omitempty"`
}

// TraceRef renders the location as "<path>" or "<path>#<symbol>".
func (e TraceEdge) TraceRef() string {
	if e.Symbol == "" {
		return e.Path
	}
	return e.Path + "#" + e.Symbol
}

// TraceWork is a story or task linked to a requirement by implements or
// modifies (the computed implemented_by / modified_by of R-LINK-8).
type TraceWork struct {
	ID ItemID `json:"id"`
	// Kind is implements or modifies, the kind written on the work item.
	Kind LinkKind `json:"kind"`
	// WholeSpec is set when the item links the whole spec rather than this
	// requirement.
	WholeSpec bool `json:"wholeSpec,omitempty"`
}

// TraceBroken is a trace: entry that no longer resolves in the working tree.
type TraceBroken struct {
	Ref RequirementRef `json:"ref"`
	// Field is "trace.code" or "trace.tests".
	Field string `json:"field"`
	// Entry is the trace ref as written.
	Entry    string   `json:"entry"`
	Code     Code     `json:"code"`
	Severity Severity `json:"severity"`
	Message  string   `json:"message"`
}

// TracedRequirement is the whole trace of one requirement: its code, its
// tests, the work linked to it and the trace: entries that are broken.
type TracedRequirement struct {
	Ref     RequirementRef `json:"ref"`
	Project ProjectKey     `json:"project,omitempty"`
	Code    []TraceEdge    `json:"code"`
	Tests   []TraceEdge    `json:"tests"`
	Work    []TraceWork    `json:"work"`
	Broken  []TraceBroken  `json:"broken,omitempty"`
}

// LineSpan is a range of changed lines on the new side of a diff: Count lines
// from Start (1-based). Count 0 is a pure deletion just before Start.
type LineSpan struct {
	Start int `json:"start"`
	Count int `json:"count"`
}

// TraceChange is one changed path handed to the reverse query. It has the
// shape of a diff entry: Path on the new side, OldPath when it was renamed,
// and the changed line spans. No spans means the whole file changed (an added
// or deleted file, or a caller that knows no lines). Symbols, when set, names
// the changed symbols directly and replaces the line-to-symbol mapping, for a
// caller that resolved them at another revision.
type TraceChange struct {
	Path    string     `json:"path"`
	OldPath string     `json:"oldPath,omitempty"`
	Lines   []LineSpan `json:"lines,omitempty"`
	Symbols []string   `json:"symbols,omitempty"`
}

// TraceHit is one trace edge a change touches, with the reason it was hit.
type TraceHit struct {
	TraceEdge
	// Reason is "file" (a whole-file edge, or a change with no lines),
	// "symbol" (a changed symbol encloses or is enclosed by the edge's),
	// "marker" (a changed line is one of the edge's marker lines),
	// "renamed" (the edge's path is the old side of a rename), "removed"
	// (the change deleted the marker or the file behind the edge) or "decl"
	// (the edge's Go function uses a package-level const, var or type the
	// change changed, GIT-US-0158).
	Reason string `json:"reason"`
	// Changed is the changed symbol that hit a symbol edge, or the changed
	// package-level name behind a decl hit.
	Changed string `json:"changed,omitempty"`
}
