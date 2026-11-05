package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// handleProjectKPIs serves GET /api/v1/projects/{key}/kpis: the workspace-home
// figures of one project (GIT-US-0205). It is the vault's "project.kpis", the
// same method browser-only mode calls into the WebAssembly module, so both
// modes show the same numbers.
func (s *Server) handleProjectKPIs(w http.ResponseWriter, r *http.Request) {
	key := chi.URLParam(r, "key")
	m, found := s.repos.forProject(key)
	if !found {
		failProblem(w, r, codeNotFound, "No mounted repository exposes project "+key+".")
		return
	}
	result, ok := s.call(w, r, m, "project.kpis", map[string]any{"project": key})
	if !ok {
		return
	}
	writeJSON(w, r, http.StatusOK, result)
}
