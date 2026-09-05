package core

import (
	"errors"
	"fmt"
	"strings"
)

// Sentinel errors returned by this package. Callers compare them with errors.Is.
var (
	// ErrInvalidFrontMatter reports a file whose YAML front matter is missing or
	// cannot be parsed into a mapping. Every ParseError wraps it.
	ErrInvalidFrontMatter = errors.New("invalid front matter")

	// ErrItemNotFound reports a lookup for an item that does not exist.
	ErrItemNotFound = errors.New("item not found")

	// ErrDuplicateID reports two files claiming the same item id (E-ID-DUPLICATE).
	ErrDuplicateID = errors.New("duplicate item id")

	// ErrRevMismatch reports a conditional write whose expected rev no longer
	// matches the bytes on disk.
	ErrRevMismatch = errors.New("rev mismatch")

	// ErrNotExist reports a path that is absent from an FS implementation.
	ErrNotExist = errors.New("file does not exist")

	// ErrReadOnly reports a write attempted on a read-only FS mount.
	ErrReadOnly = errors.New("file system is read-only")
)

// Code is a stable diagnostic identifier from docs/03-data-model.md, section 16.
// Codes starting with "E-" are errors and block writes; codes starting with "W-"
// are warnings and are only reported.
type Code string

// Diagnostic codes produced by this package.
const (
	CodeFMMissing       Code = "E-FM-MISSING"
	CodeFMYAML          Code = "E-FM-YAML"
	CodeFMType          Code = "E-FM-TYPE"
	CodeIDMissing       Code = "E-ID-MISSING"
	CodeIDGrammar       Code = "E-ID-GRAMMAR"
	CodeIDKey           Code = "E-ID-KEY"
	CodeIDTypeCode      Code = "E-ID-TYPECODE"
	CodeIDFilename      Code = "E-ID-FILENAME"
	CodeIDDuplicate     Code = "E-ID-DUPLICATE"
	CodeTitle           Code = "E-TITLE"
	CodeStatusUnknown   Code = "E-STATUS-UNKNOWN"
	CodeDateFormat      Code = "E-DATE-FORMAT"
	CodeDateOrder       Code = "E-DATE-ORDER"
	CodeFieldType       Code = "E-FIELD-TYPE"
	CodeEnum            Code = "E-ENUM"
	CodeCommentMismatch Code = "E-CMT-ITEM-MISMATCH"

	CodeProjMissing          Code = "E-PROJ-MISSING"
	CodeProjKey              Code = "E-PROJ-KEY"
	CodeProjSchema           Code = "E-PROJ-SCHEMA"
	CodeProjStatusDup        Code = "E-PROJ-STATUS-DUP"
	CodeProjStatusCategory   Code = "E-PROJ-STATUS-CATEGORY"
	CodeProjInitial          Code = "E-PROJ-INITIAL"
	CodeProjTransitionTarget Code = "E-PROJ-TRANSITION-TARGET"

	// Team-repository codes (docs/04 section 3.5). The rules that need a local
	// clone (W-TEAM-KEY-MISMATCH) are raised by the workspace, not by the parser.
	CodeTeamSchema            Code = "E-TEAM-SCHEMA"
	CodeTeamKey               Code = "E-TEAM-KEY"
	CodeTeamKeyDup            Code = "E-TEAM-KEY-DUP"
	CodeTeamHandleDup         Code = "E-TEAM-HANDLE-DUP"
	CodeTeamEmailDup          Code = "E-TEAM-EMAIL-DUP"
	CodeTeamMemberFields      Code = "E-TEAM-MEMBER-FIELDS"
	CodeTeamProjectFields     Code = "E-TEAM-PROJECT-FIELDS"
	CodeTeamBacklogInTeamRepo Code = "E-TEAM-BACKLOG-IN-TEAM-REPO"
	CodeTeamKeyMismatch       Code = "W-TEAM-KEY-MISMATCH"
	CodeTeamWebURL            Code = "W-TEAM-WEB-URL"

	// Board codes (docs/04 section 5.10). The WIP condition is live and is
	// reported by the rendered view, never by a file check.
	CodeBoardID              Code = "E-BOARD-ID"
	CodeBoardKind            Code = "E-BOARD-KIND"
	CodeBoardColumns         Code = "E-BOARD-COLUMNS"
	CodeBoardColMapping      Code = "E-BOARD-COL-MAPPING"
	CodeBoardStatusAmbiguous Code = "E-BOARD-STATUS-AMBIGUOUS"
	CodeBoardSprintKind      Code = "E-BOARD-SPRINT-KIND"
	CodeBoardUnknownProject  Code = "W-BOARD-UNKNOWN-PROJECT"
	CodeBoardRefFormat       Code = "W-BOARD-REF-FORMAT"
	CodeBoardRefDead         Code = "W-BOARD-REF-DEAD"
	CodeBoardWipExceeded     Code = "W-BOARD-WIP-EXCEEDED"
	CodeBoardUnmappedStatus  Code = "W-BOARD-UNMAPPED-STATUS"

	// Sprint codes (docs/04 section 8.4). A sprint is team-repo state, so a
	// reference into a project nobody cloned is never an error.
	CodeSprintID                Code = "E-SPRINT-ID"
	CodeSprintDates             Code = "E-SPRINT-DATES"
	CodeSprintBoard             Code = "E-SPRINT-BOARD"
	CodeSprintState             Code = "E-SPRINT-STATE"
	CodeSprintTwoActive         Code = "W-SPRINT-TWO-ACTIVE"
	CodeSprintOverlap           Code = "W-SPRINT-OVERLAP"
	CodeSprintRefDead           Code = "W-SPRINT-REF-DEAD"
	CodeSprintRefUnknownProject Code = "W-SPRINT-REF-UNKNOWN-PROJECT"

	// Retro codes (docs/04 section 9.5). A retro is team-repo state, so a
	// promoted task inside a project nobody cloned is never an error.
	CodeRetroID                 Code = "E-RETRO-ID"
	CodeRetroDate               Code = "E-RETRO-DATE"
	CodeRetroState              Code = "E-RETRO-STATE"
	CodeRetroVoteTheme          Code = "E-RETRO-VOTE-THEME"
	CodeRetroActionIDDup        Code = "E-RETRO-ACTION-ID-DUP"
	CodeRetroVoteBudget         Code = "W-RETRO-VOTE-BUDGET"
	CodeRetroVoteNonParticipant Code = "W-RETRO-VOTE-NONPARTICIPANT"
	CodeRetroActionNoOwner      Code = "W-RETRO-ACTION-NO-OWNER"
	CodeRetroActionTaskDead     Code = "W-RETRO-ACTION-TASK-DEAD"
	CodeRetroSprintDead         Code = "W-RETRO-SPRINT-DEAD"

	// Index-snapshot codes (docs/04 section 6). A snapshot is a cache, so only
	// a file that cannot be used at all is an error; the rest is advisory.
	CodeSnapMalformed   Code = "E-SNAP-MALFORMED"
	CodeSnapKeyMismatch Code = "W-SNAP-KEY-MISMATCH"
	CodeSnapStale       Code = "W-SNAP-STALE"
	CodeSnapDirty       Code = "W-SNAP-DIRTY"
	CodeSnapMissing     Code = "W-SNAP-MISSING"

	CodeWarnNoDone       Code = "W-PROJ-NO-DONE"
	CodeWarnLabelDup     Code = "W-PROJ-LABEL-DUP"
	CodeWarnCounterStale Code = "W-PROJ-COUNTER-STALE"
	CodeWarnSlugStale    Code = "W-SLUG-STALE"
)

// Severity classifies a Diagnostic.
type Severity string

// Diagnostic severities.
const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
)

// Diagnostic is a single validation finding about a file or a configuration.
type Diagnostic struct {
	Code     Code     `json:"code"`
	Severity Severity `json:"severity"`
	Path     string   `json:"path,omitempty"`
	Field    string   `json:"field,omitempty"`
	Line     int      `json:"line,omitempty"`
	Message  string   `json:"message"`
}

// String renders the diagnostic in the one-line form used by gintrack doctor.
func (d Diagnostic) String() string {
	var b strings.Builder
	b.WriteString(string(d.Code))
	b.WriteString(" ")
	if d.Path != "" {
		b.WriteString(d.Path)
		if d.Line > 0 {
			fmt.Fprintf(&b, ":%d", d.Line)
		}
		b.WriteString(" ")
	}
	if d.Field != "" {
		fmt.Fprintf(&b, "field %q: ", d.Field)
	}
	b.WriteString(d.Message)
	return b.String()
}

// ParseError reports a file that cannot be turned into an Item or a Comment.
// It carries the diagnostic code, the file path and, when the problem can be
// attributed to a node of the front matter, the line it was found on.
type ParseError struct {
	Path  string // file that failed to parse
	Line  int    // 1-based line in the file, 0 when unknown
	Field string // front-matter field, empty when the problem is structural
	Code  Code   // stable diagnostic code, e.g. E-FM-YAML
	Msg   string // human-readable explanation, lowercase, no trailing period
	Err   error  // wrapped cause, may be nil
}

// Error implements the error interface.
func (e *ParseError) Error() string {
	var b strings.Builder
	b.WriteString(e.Path)
	if e.Line > 0 {
		fmt.Fprintf(&b, ":%d", e.Line)
	}
	fmt.Fprintf(&b, ": %s: ", e.Code)
	if e.Field != "" {
		fmt.Fprintf(&b, "field %q: ", e.Field)
	}
	b.WriteString(e.Msg)
	return b.String()
}

// Unwrap reports ErrInvalidFrontMatter so that callers can classify any parse
// failure with a single errors.Is check, and keeps the original cause reachable.
func (e *ParseError) Unwrap() []error {
	if e.Err != nil {
		return []error{ErrInvalidFrontMatter, e.Err}
	}
	return []error{ErrInvalidFrontMatter}
}

// Diagnostic converts the error into an error-severity Diagnostic.
func (e *ParseError) Diagnostic() Diagnostic {
	return Diagnostic{
		Code:     e.Code,
		Severity: SeverityError,
		Path:     e.Path,
		Field:    e.Field,
		Line:     e.Line,
		Message:  e.Msg,
	}
}

// newParseError builds a *ParseError for the given file.
func newParseError(path string, line int, field string, code Code, msg string, cause error) *ParseError {
	return &ParseError{Path: path, Line: line, Field: field, Code: code, Msg: msg, Err: cause}
}
