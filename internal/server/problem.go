package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5/middleware"

	"github.com/digiogithub/git-in-track/internal/core"
	"github.com/digiogithub/git-in-track/internal/vault"
)

// The problem codes this package raises on its own, on top of the ones the
// vault reports. They are part of the stable catalog of docs/07 section 5.4.
const (
	codeNotFound             = "not_found"
	codeUnauthorized         = "unauthorized"
	codeInvalidRequest       = "invalid_request"
	codeNotImplemented       = "not_implemented"
	codePreconditionRequired = "precondition_required"
	codeRepoNotRegistered    = "repo_not_registered"
	codeIndexUnavailable     = "index_unavailable"
	codeInternal             = "internal"
)

// problemField is one per-field validation failure of a problem document.
type problemField struct {
	Field   string `json:"field,omitempty"`
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

// problem is an RFC 7807 problem document, with the machine-readable `code`
// clients switch on (docs/07 section 5.4).
type problem struct {
	Type       string         `json:"type"`
	Title      string         `json:"title"`
	Status     int            `json:"status"`
	Detail     string         `json:"detail,omitempty"`
	Instance   string         `json:"instance,omitempty"`
	Code       string         `json:"code"`
	RequestID  string         `json:"requestId,omitempty"`
	Path       string         `json:"path,omitempty"`
	CurrentRev string         `json:"currentRev,omitempty"`
	Errors     []problemField `json:"errors,omitempty"`
}

// statusForCode maps the stable error catalog onto HTTP status codes. Anything
// unknown is an internal error: a client that has never heard of a code still
// learns whether it may retry.
func statusForCode(code string) int {
	switch code {
	case "stale_revision", "conflict":
		return http.StatusPreconditionFailed
	case codeNotFound, "unknown_method", codeRepoNotRegistered, "repo_not_cloned":
		return http.StatusNotFound
	case "validation_failed", "workflow_transition_denied", "invalid_front_matter":
		return http.StatusUnprocessableEntity
	case "duplicate_id", "wip_limit_exceeded":
		// A WIP limit is advisory: the move is refused once, and the caller may
		// repeat it with `force` (docs/04 R-COL-5).
		return http.StatusConflict
	case "read_only", "forbidden":
		return http.StatusForbidden
	case codeInvalidRequest:
		return http.StatusBadRequest
	case codeUnauthorized:
		return http.StatusUnauthorized
	case codePreconditionRequired:
		return http.StatusPreconditionRequired
	case codeNotImplemented:
		return http.StatusNotImplemented
	case codeIndexUnavailable:
		return http.StatusServiceUnavailable
	case "rate_limited":
		return http.StatusTooManyRequests
	default:
		return http.StatusInternalServerError
	}
}

// titleForCode renders a code as the human-readable title of the document:
// "stale_revision" becomes "Stale revision".
func titleForCode(code string) string {
	words := strings.ReplaceAll(code, "_", " ")
	if words == "" {
		return "Error"
	}
	return strings.ToUpper(words[:1]) + words[1:]
}

// writeProblem writes an application/problem+json response from a code and a
// detail message, filling in the status and title the catalog dictates.
func writeProblem(w http.ResponseWriter, r *http.Request, status int, code, title, detail string) {
	writeProblemDoc(w, r, problem{Status: status, Code: code, Title: title, Detail: detail})
}

// failf writes the problem a code implies, with a formatted detail message.
func failProblem(w http.ResponseWriter, r *http.Request, code, detail string) {
	writeProblemDoc(w, r, problem{Code: code, Detail: detail})
}

// writeProblemDoc completes a partially filled problem document and writes it.
func writeProblemDoc(w http.ResponseWriter, r *http.Request, body problem) {
	if body.Code == "" {
		body.Code = codeInternal
	}
	if body.Status == 0 {
		body.Status = statusForCode(body.Code)
	}
	if body.Title == "" {
		body.Title = titleForCode(body.Code)
	}
	if body.Type == "" {
		body.Type = problemBase + strings.ReplaceAll(body.Code, "_", "-")
	}
	if body.Instance == "" {
		body.Instance = r.URL.Path
	}
	if id := middleware.GetReqID(r.Context()); id != "" {
		body.RequestID = id
		w.Header().Set("X-Request-Id", id)
	}
	w.Header().Set("Content-Type", "application/problem+json; charset=utf-8")
	w.WriteHeader(body.Status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		// The status line is already on the wire; the request logger records
		// the truncated body.
		_ = err
	}
}

// writeVaultError maps a failure of the shared core onto a problem document.
// The stable code comes from vault.AsError, which every host switches on; the
// extra fields come from the underlying core errors, so that a client can retry
// a rejected write without a second round trip.
func writeVaultError(w http.ResponseWriter, r *http.Request, err error) {
	classified, ok := vault.AsError(err)
	if !ok {
		failProblem(w, r, codeInternal, "The request failed for an unknown reason.")
		return
	}
	doc := problem{
		Code:   classified.Code,
		Detail: classified.Message,
		Path:   classified.Path,
	}

	var stale *core.StaleRevisionError
	if errors.As(err, &stale) {
		doc.CurrentRev = string(stale.Current)
	}
	var diag *core.DiagnosticError
	if errors.As(err, &diag) {
		doc.Errors = []problemField{{
			Field:   diag.Diagnostic.Field,
			Code:    string(diag.Diagnostic.Code),
			Message: diag.Diagnostic.Message,
		}}
	}
	var parse *core.ParseError
	if errors.As(err, &parse) {
		doc.Errors = []problemField{{
			Field:   parse.Field,
			Code:    string(parse.Code),
			Message: parse.Msg,
		}}
	}
	writeProblemDoc(w, r, doc)
}
