package mcp

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

// Pagination and projection, the two things that make a tool result cheap.
//
// `item.list` already paginates inside the core, and its cursor is passed
// through untouched. Everything the core answers as a whole slice — a search,
// the knowledge-base tree — is paged here with an opaque offset cursor bound to
// the filter that produced it, exactly as docs/08-mcp-server.md section 3.4
// describes: presenting a cursor with a different filter is an error, not a
// silently wrong page.

// Page-size bounds. A tool never returns more than maxPageSize entries however
// large a limit the client asks for: an agent that wants everything walks the
// cursor, which keeps a single result inside a sane token budget.
const (
	defaultPageSize = 20
	maxPageSize     = 100
)

// boundedLimit clamps a requested page size into the documented range. Zero and
// negative values mean "the default", not "everything".
func boundedLimit(requested int) int {
	if requested <= 0 {
		return defaultPageSize
	}
	if requested > maxPageSize {
		return maxPageSize
	}
	return requested
}

// cursor is the opaque continuation token of a list this package pages itself.
// It carries the offset it resumes at and a fingerprint of the filter it was
// issued for, so that a cursor cannot be replayed against a different query and
// quietly skip or repeat results.
type cursor struct {
	Offset int    `json:"o"`
	Filter string `json:"f"`
}

// encodeCursor renders the continuation token for the next page, or the empty
// string when there is none.
func encodeCursor(offset int, filter string) string {
	raw, err := json.Marshal(cursor{Offset: offset, Filter: filter})
	if err != nil {
		// cursor holds an int and a string; marshaling cannot fail.
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

// decodeCursor reads a continuation token and checks it against the filter of
// the current call. An empty token starts at offset zero.
func decodeCursor(token, filter string) (int, error) {
	if token == "" {
		return 0, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return 0, failf(codeInvalidCursor, "cursor %q is not a cursor this server issued", token)
	}
	var c cursor
	if err := json.Unmarshal(raw, &c); err != nil {
		return 0, failf(codeInvalidCursor, "cursor %q is not a cursor this server issued", token)
	}
	if c.Filter != filter {
		return 0, &toolError{
			Code: codeInvalidCursor,
			Message: "the cursor was issued for a different query; " +
				"restart the walk without a cursor after changing any filter",
			Field: "cursor",
		}
	}
	if c.Offset < 0 {
		return 0, failf(codeInvalidCursor, "cursor %q carries a negative offset", token)
	}
	return c.Offset, nil
}

// fingerprint hashes the filter a cursor belongs to. It is short on purpose:
// the cursor travels in every page of a walk, and eight bytes are plenty to
// notice that the query changed.
func fingerprint(parts ...any) string {
	sum := sha256.Sum256([]byte(fmt.Sprint(parts...)))
	return hex.EncodeToString(sum[:])[:8]
}

// slice returns the requested page of items and the cursor for the next one.
// It never returns a cursor for a page that ends the list, so an agent's walk
// terminates without an extra empty call.
func slice[T any](all []T, offset, limit int, filter string) (page []T, next string) {
	if offset >= len(all) {
		return nil, ""
	}
	end := offset + limit
	if end >= len(all) {
		return all[offset:], ""
	}
	return all[offset:end], encodeCursor(end, filter)
}

// includes reports whether a requested `include` list holds a name, comparing
// case-insensitively so that "Body" and "body" behave alike.
func includes(list []string, name string) bool {
	for _, entry := range list {
		if strings.EqualFold(strings.TrimSpace(entry), name) {
			return true
		}
	}
	return false
}
