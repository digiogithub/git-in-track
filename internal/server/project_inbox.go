package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/digiogithub/git-in-track/internal/gitops"
	"github.com/digiogithub/git-in-track/internal/vault"
)

// handleProjectInboxEnable serves POST /api/v1/projects/{key}/inbox: it adds the
// triage status to a project created before the inbox existed (GIT-US-0100,
// ADR-033). The edit is the vault's "project.inbox.enable", the same method the
// browser calls into the WebAssembly module.
//
// If-Match is optional and carries the `configRev` a project listing reports.
// The write cannot lose anything — it only ever inserts one status and refuses
// a workflow that already has one — so an absent header is unconditional, like
// scaffolding a project is.
func (s *Server) handleProjectInboxEnable(w http.ResponseWriter, r *http.Request) {
	key := chi.URLParam(r, "key")
	m, found := s.repos.forProject(key)
	if !found {
		failProblem(w, r, codeNotFound, "No mounted repository exposes project "+key+".")
		return
	}
	rev, _, _ := ifMatch(r)
	result, ok := s.call(w, r, m, "project.inbox.enable", vault.ProjectInboxEnableParams{Project: key, Rev: rev})
	if !ok {
		return
	}
	enabled, ok := result.(vault.ProjectInboxEnabled)
	if !ok {
		writeJSON(w, r, http.StatusOK, result)
		return
	}
	// project.yaml changed what every view of the project renders, so the
	// refresh is a full one, and commit-on-save stages the file like any write.
	sets := []vault.RepoWriteSet{{
		VaultID: m.id, Written: enabled.Writes.Written, Removed: enabled.Writes.Removed,
	}}
	s.publishWriteSets(r, sets)
	s.publishIndexUpdated(m, indexCounts{Full: true}, requestIDOf(r))
	s.commitWriteSets(r.Context(), sets, sprintFields(key, "project inbox", gitops.ActionUpdate))
	writeEntity(w, r, http.StatusOK, enabled, enabled.Project.ConfigRev)
}
