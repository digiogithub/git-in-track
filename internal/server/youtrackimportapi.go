package server

import (
	"errors"
	"net/http"
	"strings"

	"github.com/digiogithub/git-in-track/internal/vault"
)

// POST /api/v1/youtrack/import and /import/preview.
//
// The HTTP layer between the vault's import (GIT-US-0047) and the import
// dialog (GIT-US-0059). It is deliberately thin: the vault decides what an
// import would do, the background engine does it, and this file does nothing
// but route, validate the project and choose between the two.
//
// The choice is the only decision here, and it is made by size:
//
//   - **preview** is synchronous. It writes nothing and reads a bounded set of
//     issues, so the dialog can wait for it and the answer is the plan.
//   - **run** is queued. An import is a hundred issues and a hundred requests
//     against somebody else's rate limit; holding an HTTP response open across
//     that is what the background engine exists to avoid. The answer is a job
//     id, and the job narrates itself on `sync.job.*` (§5.6).
//
// Both take `internal/vault.YouTrackImportParams` as the body and the
// `?key=<gintrackProjectKey>` convention of the rest of this subtree.

// youtrackImportJobAnswer is what a queued import answers with.
//
// It carries the job id and nothing else that could go stale: what the import
// then does is reported by the engine's own events and by
// `GET /api/v1/sync/jobs/{id}`, and duplicating any of it here would be a
// second, lagging copy of the queue.
type youtrackImportJobAnswer struct {
	// JobID is the engine job the import runs as.
	JobID string `json:"jobId"`
	// ProjectKey is the git-in-track project the issues will land in, and
	// Repo the mounted repository that holds it.
	ProjectKey string `json:"projectKey"`
	Repo       string `json:"repo,omitempty"`
	// Queued is always true. It is spelled out so that a client reading either
	// this shape or a synchronous vault.YouTrackImportResult can tell them
	// apart without inspecting which keys are present.
	Queued bool `json:"queued"`
}

// handleYouTrackImportPreview serves POST /api/v1/youtrack/import/preview.
//
// It is synchronous because it is a read: the plan says what each issue would
// become, which item an update would patch and what could not be resolved, and
// none of it is written.
func (s *Server) handleYouTrackImportPreview(w http.ResponseWriter, r *http.Request) {
	m, key, params, ok := s.youtrackImportRequest(w, r)
	if !ok {
		return
	}
	preview, err := m.vlt.YouTrackImportPreview(r.Context(), params)
	if err != nil {
		s.failYouTrackImport(w, r, key, err)
		return
	}
	writeJSON(w, r, http.StatusOK, preview)
}

// handleYouTrackImport serves POST /api/v1/youtrack/import.
//
// It queues. The answer is `{jobId, projectKey, repo, queued}` and the work
// happens on the `youtrack.import` job kind, where the retry ladder, the shared
// rate limiter and the journal already are.
func (s *Server) handleYouTrackImport(w http.ResponseWriter, r *http.Request) {
	m, key, params, ok := s.youtrackImportRequest(w, r)
	if !ok {
		return
	}
	// The connection is resolved before anything is queued, so that a project
	// with no token fails in the call the user can still see rather than inside
	// a job nobody is watching.
	if _, _, err := s.youtrack.clientFor(key); err != nil {
		s.failYouTrackImport(w, r, key, err)
		return
	}
	jobID, err := s.EnqueueYouTrackImport(r.Context(), YouTrackImportRequest{
		Repo: m.id, Project: key,
		Params: YouTrackImportParams{
			Query:              params.Query,
			IDs:                params.IDs,
			Depth:              params.Depth,
			IncludeLinks:       params.IncludeLinks,
			IncludeComments:    params.IncludeComments,
			IncludeAttachments: params.IncludeAttachments,
		},
	})
	if err != nil {
		s.log.Warn("an import could not be queued", "project", key, "error", err)
		failProblem(w, r, codeSyncEngineNotRunning,
			"The background job engine is not running on this companion, so the import could not be queued.")
		return
	}
	writeJSON(w, r, http.StatusAccepted, youtrackImportJobAnswer{
		JobID: jobID, ProjectKey: key, Repo: m.id, Queued: true,
	})
}

// youtrackImportRequest resolves the project, the mount and the body both
// import routes take. It writes the problem document itself and reports whether
// the request is usable.
func (s *Server) youtrackImportRequest(
	w http.ResponseWriter, r *http.Request,
) (*mount, string, vault.YouTrackImportParams, bool) {
	key, ok := s.youtrackProjectKey(w, r)
	if !ok {
		return nil, "", vault.YouTrackImportParams{}, false
	}
	var params vault.YouTrackImportParams
	if r.ContentLength != 0 && !decodeBody(w, r, &params) {
		return nil, "", vault.YouTrackImportParams{}, false
	}
	// The project of the body and the project of the query string are the same
	// thing; the query string wins, because that is what the rest of this
	// subtree addresses a project by.
	params.Project = key

	m, found := s.repos.forProject(key)
	if !found {
		failProblem(w, r, codeNotFound, "No mounted repository exposes "+key+".")
		return nil, "", vault.YouTrackImportParams{}, false
	}
	if !m.ready() {
		failProblem(w, r, codeIndexUnavailable, "Repository "+m.id+" is not indexed.")
		return nil, "", vault.YouTrackImportParams{}, false
	}
	return m, key, params, true
}

// failYouTrackImport maps a failure onto the problem catalog.
//
// A vault error carrying its own code — `invalid_request` from a malformed
// parameter, `unavailable` from a host with no tracker — is rendered by the
// vault error writer, which already knows those codes. Everything else is a
// YouTrack failure and goes through failYouTrack, which never echoes the cause
// of a client error and therefore never echoes a token.
func (s *Server) failYouTrackImport(w http.ResponseWriter, r *http.Request, key string, err error) {
	var verr *vault.Error
	if errors.As(err, &verr) && !isYouTrackVaultCode(verr.Code) {
		writeVaultError(w, r, err)
		return
	}
	if strings.Contains(err.Error(), key) {
		s.log.Debug("an import failed", "project", key)
	}
	s.failYouTrack(w, r, err)
}

// isYouTrackVaultCode reports whether a vault error code stands for a failure
// of the connection rather than of the request, in which case the YouTrack
// problem catalog describes it better than the generic one.
func isYouTrackVaultCode(code string) bool {
	switch code {
	case "unavailable", "youtrack_not_configured":
		return true
	default:
		return false
	}
}
