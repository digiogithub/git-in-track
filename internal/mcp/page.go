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
// Every cursor this package hands out is an opaque token bound to the filter
// that produced it, exactly as docs/08-mcp-server.md section 3, principle 4, describes:
// presenting a cursor with a different filter is an invalid_cursor error, not a
// silently wrong page. Lists the core answers as a whole slice — a search, the
// knowledge-base tree — are paged here with an offset. Lists the core paginates
// itself (`item.list`, `inbox.list`) keep the core's own cursor, which is bound
// only to the sort, wrapped inside a token that also carries the fingerprint of
// every filter the tool was called with (GIT-US-0155).

// Page-size bounds. A tool never returns more than maxPageSize entries however
// large a limit the client asks for: an agent that wants everything walks the
// cursor, which keeps a single result inside a sane token budget.
const (
	defaultPageSize = 20
	maxPageSize     = 100
)

// boundedLimit clamps a requested page size into the documented range. Zero and
// negative values mean "the default", not "everything".
// Implements: GIT-SP-0004.R1
func boundedLimit(requested int) int {
	if requested <= 0 {
		return defaultPageSize
	}
	if requested > maxPageSize {
		return maxPageSize
	}
	return requested
}

// cursor is the opaque continuation token of a list. It carries a fingerprint
// of the filter it was issued for, so that a cursor cannot be replayed against
// a different query and quietly skip or repeat results, plus either the offset
// it resumes at (a list this package pages itself) or the core's own cursor (a
// list the core pages).
type cursor struct {
	Offset int    `json:"o"`
	Filter string `json:"f"`
	Inner  string `json:"c,omitempty"`
}

// encodeCursor renders the continuation token for the next page, or the empty
// string when there is none.
func encodeCursor(offset int, filter string) string {
	return renderCursor(cursor{Offset: offset, Filter: filter})
}

// wrapCursor binds a cursor the core issued to the filter of the current call.
// An empty core cursor means the walk is over, and stays empty.
func wrapCursor(inner, filter string) string {
	if inner == "" {
		return ""
	}
	return renderCursor(cursor{Filter: filter, Inner: inner})
}

func renderCursor(c cursor) string {
	raw, err := json.Marshal(c)
	if err != nil {
		// cursor holds an int and two strings; marshaling cannot fail.
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

// decodeCursor reads an offset token and checks it against the filter of the
// current call. An empty token starts at offset zero.
// Implements: GIT-SP-0004.R4

func decodeCursor(token, filter string) (int, error) {
	c, err := parseCursor(token, filter)
	if err != nil || token == "" {
		return 0, err
	}
	if c.Inner != "" {
		return 0, failf(codeInvalidCursor, "cursor %q belongs to another tool", token)
	}
	if c.Offset < 0 {
		return 0, failf(codeInvalidCursor, "cursor %q carries a negative offset", token)
	}
	return c.Offset, nil
}

// unwrapCursor reads a token made by wrapCursor, checks it against the filter
// of the current call and returns the core cursor to resume from. An empty
// token starts the walk.
func unwrapCursor(token, filter string) (string, error) {
	c, err := parseCursor(token, filter)
	if err != nil || token == "" {
		return "", err
	}
	if c.Inner == "" {
		return "", failf(codeInvalidCursor, "cursor %q belongs to another tool", token)
	}
	return c.Inner, nil
}

// parseCursor decodes a token and refuses it unless it was issued for filter.
func parseCursor(token, filter string) (cursor, error) {
	if token == "" {
		return cursor{}, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return cursor{}, failf(codeInvalidCursor, "cursor %q is not a cursor this server issued", token)
	}
	var c cursor
	if err := json.Unmarshal(raw, &c); err != nil {
		return cursor{}, failf(codeInvalidCursor, "cursor %q is not a cursor this server issued", token)
	}
	if c.Filter != filter {
		return cursor{}, &toolError{
			Code: codeInvalidCursor,
			Message: "the cursor was issued for a different query; " +
				"restart the walk without a cursor after changing any filter",
			Field: "cursor",
		}
	}
	return c, nil
}

// fingerprint hashes the filter a cursor belongs to. The parts are encoded as
// JSON rather than printed, so that ["a b"] and ["a", "b"] do not collide. It
// is short on purpose: the cursor travels in every page of a walk, and eight
// bytes are plenty to notice that the query changed.
// Implements: GIT-SP-0004.R4

func fingerprint(parts ...any) string {
	raw, err := json.Marshal(parts)
	if err != nil {
		raw = []byte(fmt.Sprint(parts...))
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])[:8]
}

// slice returns the requested page of items and the cursor for the next one.
// It never returns a cursor for a page that ends the list, so an agent's walk
// terminates without an extra empty call.
// Implements: GIT-SP-0004.R2, GIT-SP-0004.R3
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
