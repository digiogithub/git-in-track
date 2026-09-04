package mcp

import (
	"encoding/json"
	"errors"
	"fmt"

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
)

// toolError is the structured failure a tool reports. The MCP SDK turns an
// error returned by a handler into a tool result with isError set and the error
// text as its content, so the text itself is the machine-readable payload:
// a compact JSON object an agent can branch on without parsing prose.
//
// The shape is the one docs/08-mcp-server.md section 3.9 specifies: a stable
// code, a human message, the offending field where there is one, and what a
// correct value would look like.
type toolError struct {
	Code     string `json:"code"`
	Message  string `json:"message"`
	Field    string `json:"field,omitempty"`
	Path     string `json:"path,omitempty"`
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
	return &toolError{Code: classified.Code, Message: classified.Message, Path: classified.Path}
}
