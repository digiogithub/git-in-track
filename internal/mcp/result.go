package mcp

import (
	"encoding/json"

	"github.com/digiogithub/git-in-track/internal/core"
)

// Decoding the answers of the shared core.
//
// A mutating method answers with the item (or page, or board) and the WriteSet
// the host must persist. This package re-encodes that value and reads back the
// half it publishes, which keeps it honest: it can only return what the core
// actually said, and a field the core stops emitting stops being returned
// instead of silently keeping a stale shape.

// writeSet is the half of vault.WriteSet a tool result exposes: which files
// changed, never their contents. An agent that wants the bytes reads the file.
type writeSet struct {
	Written []struct {
		Path string `json:"path"`
	} `json:"written"`
	Removed []string `json:"removed"`
}

// paths lists the vault-relative files a write touched, written first and then
// removed, so the order is stable across calls.
func (w writeSet) paths() []string {
	out := make([]string, 0, len(w.Written)+len(w.Removed))
	for _, f := range w.Written {
		out = append(out, f.Path)
	}
	out = append(out, w.Removed...)
	if len(out) == 0 {
		return nil
	}
	return out
}

// decodeResult reads the value a core method returned into T.
func decodeResult[T any](result any) (T, error) {
	var out T
	raw, err := json.Marshal(result)
	if err != nil {
		return out, failf("internal", "the answer of the core could not be encoded: %v", err)
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, failf("internal", "the answer of the core could not be decoded: %v", err)
	}
	return out, nil
}

// writeResultOf projects the answer of an item mutation onto the wire shape.
// The item comes back whole — an agent that just wrote it usually wants to see
// what it now looks like — with its new rev, which is the token the next write
// has to quote.
func writeResultOf(result any) (WriteResult, error) {
	payload, err := decodeResult[struct {
		Item   core.Item `json:"item"`
		Writes writeSet  `json:"writes"`
	}](result)
	if err != nil {
		return WriteResult{}, err
	}
	item := itemOf(payload.Item)
	item.Body = ""
	return WriteResult{
		Item:    projectItem(item, append(append([]string{}, defaultItemFields...), "path", "links", "milestone")),
		Changed: payload.Writes.paths(),
	}, nil
}
