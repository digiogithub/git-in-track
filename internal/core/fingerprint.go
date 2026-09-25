package core

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// Fingerprint hashes the query a cursor belongs to, so that a cursor presented
// with a different query is refused instead of resuming a walk over another
// result set (docs/08 section 3, principle 4). Every paginated surface binds
// its cursors through it: the core's item query, and the lists the MCP server
// pages itself.
//
// The parts are encoded as JSON rather than printed, so that ["a b"] and
// ["a", "b"] do not collide. The hash is short on purpose: a cursor travels in
// every page of a walk, and eight hex digits are plenty to notice that the
// query changed. It is a guard against a mistake, not against forgery.
// Implements: GIT-SP-0004.R4, GIT-SP-0004.R5
func Fingerprint(parts ...any) string {
	raw, err := json.Marshal(parts)
	if err != nil {
		raw = []byte(fmt.Sprint(parts...))
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])[:8]
}
