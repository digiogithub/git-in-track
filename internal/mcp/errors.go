package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/digiogithub/git-in-track/internal/core"
	"github.com/digiogithub/git-in-track/internal/vault"
)

// Error codes this package adds to the catalog internal/vault classifies into.
// Everything else is passed through unchanged, so an agent sees the same code
// the REST API returns for the same mistake.
const (
	// codeInvalidRequest is a malformed or contradictory tool argument.
	codeInvalidRequest = "invalid_request"
	// codeForbiddenPath is a path argument that leaves the vault root.
	codeForbiddenPath = "forbidden_path"
	// codeWriteDisabled is a write tool reached on a read-only server. Write
	// tools are normally absent from tools/list, so this is only seen by a
	// client holding a stale list.
	codeWriteDisabled = "write_disabled"
	// codeInvalidCursor is a cursor that does not belong to the filter it was
	// presented with.
	codeInvalidCursor = "invalid_cursor"
	// codeNotFound is an item, page or board that is not indexed.
	codeNotFound = "not_found"
	// codePreconditionRequired is a write tool called without the rev of the
	// read it is based on. It is the same refusal the REST API returns as 428
	// for a mutation with no If-Match (docs/07 section 5.3).
	codePreconditionRequired = "precondition_required"
)

// wildcardRev is the rev that deliberately skips the optimistic lock, spelled
// as the If-Match wildcard of RFC 9110 so that one value means the same thing
// on both surfaces. It is unsafe by design and has to be typed out: a write
// that omits rev altogether is refused, never treated as unconditional.
const wildcardRev = "*"

// toolError is the structured failure a tool reports. The MCP SDK turns an
// error returned by a handler into a tool result with isError set and the error
// text as its content, so the text itself is the machine-readable payload:
// a compact JSON object an agent can branch on without parsing prose.
//
// The shape is the one docs/08-mcp-server.md section 3.9 specifies: a stable
// code, a human message, the offending field where there is one, and what a
// correct value would look like.
type toolError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Field   string `json:"field,omitempty"`
	Path    string `json:"path,omitempty"`
	// CurrentRev is the revision the file holds now. It is present on every
	// stale_revision, which is what makes a retry one round trip: re-read only
	// if the agent needs the new content, otherwise quote this value.
	CurrentRev string `json:"currentRev,omitempty"`
	// Conflicts are the fields the refused write would still have changed,
	// judged against the content on disk now. An empty list on a stale revision
	// means the write had already been applied by someone else.
	Conflicts []core.ConflictField `json:"conflicts,omitempty"`
	// Retry tells the agent what to do next, in one sentence. It is a constant
	// per code, not a paraphrase of the message.
	Retry    string `json:"retry,omitempty"`
	Expected any    `json:"expected,omitempty"`
}

// Error renders the failure as the compact JSON object the client receives.
func (e *toolError) Error() string {
	raw, err := json.Marshal(struct {
		Error *toolError `json:"error"`
	}{Error: e})
	if err != nil {
		// Every field is a string or a value the caller built in this package,
		// so this is unreachable; fall back to the message rather than panic.
		return e.Code + ": " + e.Message
	}
	return string(raw)
}

// failf builds a tool error with a formatted message.
func failf(code, format string, args ...any) *toolError {
	return &toolError{Code: code, Message: fmt.Sprintf(format, args...)}
}

// invalidField builds the error for an argument the core would reject anyway,
// telling the agent which field was wrong and what belongs in it.
func invalidField(field, message string, expected any) *toolError {
	return &toolError{Code: codeInvalidRequest, Message: message, Field: field, Expected: expected}
}

// fromVault translates a failure of the shared core into a tool error, keeping
// the code the REST API and the browser report for the same mistake. A rule
// that exists in MCP but not in the rest of the product would be a bug, and so
// would an error code.
func fromVault(err error) error {
	if err == nil {
		return nil
	}
	var already *toolError
	if errors.As(err, &already) {
		return already
	}
	classified, ok := vault.AsError(err)
	if !ok {
		return nil
	}
	out := &toolError{Code: classified.Code, Message: classified.Message, Path: classified.Path}
	if classified.Code == core.StaleRevisionCode {
		out.CurrentRev = classified.Current
		out.Conflicts = classified.Conflicts
		out.Retry = staleRetry
	}
	return out
}

// staleRetry is the recovery protocol an agent follows after a lost race. It is
// spelled out in the error because that is the only text a model is guaranteed
// to read at the moment it matters.
const staleRetry = "Someone else wrote this file first. Re-read the item with get_item, " +
	"decide whether your change is still wanted given `conflicts`, then repeat the write " +
	"quoting `currentRev`. Never retry by sending rev \"*\": that overwrites their work."

// requiredRev validates the optimistic lock a write tool carries and returns
// the value to hand the core: the rev itself, or an empty string for the
// wildcard, which is how the core spells "write unconditionally". A missing rev
// is refused here rather than passed on, because the core treats an empty rev
// as an unconditional write and an agent must never reach that by omission.
func requiredRev(field, value string) (string, error) {
	switch trimmed := strings.TrimSpace(value); trimmed {
	case "":
		return "", &toolError{
			Code: codePreconditionRequired,
			Message: "this write needs the " + field +
				" of the read it is based on, so a concurrent edit cannot be lost",
			Field: field,
			Expected: "a rev from a previous read, for example sha256:11c35de07a9b2f60; " +
				"or \"*\" to overwrite whatever is there now",
			Retry: "Read the item first (get_item) and quote the rev it returns.",
		}
	case wildcardRev:
		return "", nil
	default:
		return trimmed, nil
	}
}
