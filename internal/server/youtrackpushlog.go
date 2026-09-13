package server

import (
	"encoding/json"

	"github.com/digiogithub/git-in-track/internal/vault"
)

// The observability half of the automatic comment push, task GIT-T-0164.
//
// The *decision* to push is not made here and must not be: `Vault.autoPushComment`
// (internal/vault/youtrackpush.go) reaches it from "comment.add", which is the
// one path every surface — REST, the web app, MCP and the CLI — writes a
// comment through. A `shouldPushComment` in this package would be a second copy
// of that rule, and a second copy is how two surfaces come to disagree.
//
// What the vault deliberately does not do is log: it compiles to WebAssembly
// and has no logger. So the server logs what the vault decided, and it logs it
// **once per item** rather than once per comment — a thread with forty comments
// pushed by `--all` is one line, not forty.

// logCommentPushDecision records that this project is pushing comments
// automatically, once for each item whose thread starts being pushed.
//
// Only the comment-push kind is interesting: a knowledge-base job is already
// narrated by the `sync.job.*` events, one per page, and a page is not a thread.
func (s *Server) logCommentPushDecision(job vault.YouTrackJob) {
	if job.Kind != vault.JobKindCommentPush {
		return
	}
	itemID := commentPushItemID(job.Payload)
	if itemID == "" {
		return
	}
	if !s.youtrack.noteCommentPushItem(itemID) {
		return
	}
	s.log.Info("pushing comments to youtrack", "project", job.Project, "item", itemID)
}

// commentPushItemID digs the item id out of a comment-push payload.
//
// The payload is a struct internal/vault keeps to itself, so it is read the way
// the engine reads it — through JSON — rather than by naming a type this
// package cannot see. A payload that does not decode is not an error worth
// reporting: the job itself carries the same bytes and fails loudly if they are
// wrong.
func commentPushItemID(payload any) string {
	if payload == nil {
		return ""
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	var decoded struct {
		ItemID string `json:"itemId"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return ""
	}
	return decoded.ItemID
}

// noteCommentPushItem reports whether this is the first comment queued for an
// item, marking it as seen. The set is per process and unbounded only in the
// number of items whose comments were pushed in one session, which is the same
// order of magnitude as the items themselves.
func (y *youtrackState) noteCommentPushItem(itemID string) bool {
	y.mu.Lock()
	defer y.mu.Unlock()
	if y.pushedItems == nil {
		y.pushedItems = map[string]bool{}
	}
	if y.pushedItems[itemID] {
		return false
	}
	y.pushedItems[itemID] = true
	return true
}
