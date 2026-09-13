package server

import (
	"net/http"

	"github.com/digiogithub/git-in-track/internal/vault"
)

// POST /api/v1/youtrack/comments/push, story GIT-US-0076.
//
// The HTTP layer between the vault's comment push (GIT-US-0068) and the "Send
// to YouTrack" action on a comment. It is as thin as the import route beside it:
// resolve the project, check the connection, hand the call to the vault, and
// answer what was queued.
//
// It **queues**, like every other outbound write to a tracker. The answer is
// `202 Accepted` carrying the job id, and `pushed` means *queued*, not
// delivered: the evidence a reader trusts is the comment's own `external`
// entry, which the job writes when the post came back.
//
// The coalescing key of the queued job is the **comment path**, not the item
// id, so a burst of edits to one comment is one push while two comments of the
// same item stay two. That is decided in `internal/vault` and repeated nowhere
// here; this file must not grow a second opinion about it.

// handleYouTrackCommentPush serves POST /api/v1/youtrack/comments/push.
func (s *Server) handleYouTrackCommentPush(w http.ResponseWriter, r *http.Request) {
	key, ok := s.youtrackProjectKey(w, r)
	if !ok {
		return
	}
	var params vault.YouTrackCommentPushParams
	if r.ContentLength != 0 && !decodeBody(w, r, &params) {
		return
	}
	// The project of the body and the project of the query string are the same
	// thing; the query string wins, as everywhere else in this subtree.
	params.Project = key

	m, found := s.repos.forProject(key)
	if !found {
		failProblem(w, r, codeNotFound, "No mounted repository exposes "+key+".")
		return
	}
	// The connection is resolved before anything is queued, so that a project
	// with no token fails in the call the user can still see rather than inside
	// a job nobody is watching — the same order the import route uses.
	if _, _, err := s.youtrack.clientFor(key); err != nil {
		s.failYouTrackImport(w, r, key, err)
		return
	}
	result, ok := s.call(w, r, m, "youtrack.comment.push", params)
	if !ok {
		return
	}
	writeJSON(w, r, http.StatusAccepted, result)
}
