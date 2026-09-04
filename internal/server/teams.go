package server

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/digiogithub/git-in-track/internal/core"
)

// handleTeams serves GET /api/v1/teams. A workspace holds at most one team
// repository, so the list has zero or one entry; it is a list anyway because
// the client must be able to tell "no team repository is open" from an error.
func (s *Server) handleTeams(w http.ResponseWriter, r *http.Request) {
	teams := []any{}
	if summary, err := s.repos.workspace().Dispatch(r.Context(), "team.get", nil); err == nil {
		teams = append(teams, summary)
	}
	writeJSON(w, r, http.StatusOK, map[string]any{"teams": teams, "total": len(teams)})
}

// handleTeam serves GET /api/v1/teams/{key}.
func (s *Server) handleTeam(w http.ResponseWriter, r *http.Request) {
	key := chi.URLParam(r, "key")
	m, ok := s.repos.workspace().TeamMount()
	if !ok {
		failProblem(w, r, codeNotFound, "No mounted repository holds a "+core.TeamFileName+".")
		return
	}
	team := m.Vault.Team()
	if team == nil || (key != "" && key != string(team.Key) && key != m.ID) {
		failProblem(w, r, codeNotFound, "No mounted team repository is called "+key+".")
		return
	}
	summary, err := s.repos.workspace().Dispatch(r.Context(), "team.get", nil)
	if err != nil {
		writeVaultError(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, summary)
}

// handleResolveRef serves GET /api/v1/refs?ref=<KEY>/<ITEM-ID>: where a
// cross-repository reference points, and whether it can be read right now.
//
// A reference into a project nobody cloned is not an error — it is the normal
// state of a team board — so the answer carries `cloned: false` and a reason
// instead of a 404.
func (s *Server) handleResolveRef(w http.ResponseWriter, r *http.Request) {
	raw := r.URL.Query().Get("ref")
	if raw == "" {
		failProblem(w, r, codeInvalidRequest, "Pass the reference to resolve as ?ref=<KEY>/<ITEM-ID>.")
		return
	}
	if _, err := core.ParseRef(raw); err != nil {
		failProblem(w, r, codeInvalidRequest, err.Error())
		return
	}
	params, err := json.Marshal(map[string]string{"ref": raw})
	if err != nil {
		failProblem(w, r, codeInvalidRequest, err.Error())
		return
	}
	result, err := s.repos.workspace().Dispatch(r.Context(), "ref.resolve", params)
	if err != nil {
		writeVaultError(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, result)
}

// handleWorkspaceTree serves GET /api/v1/workspace: every open repository with
// its projects, the team repository among them and the findings only a
// multi-repository view can make (a project key served twice, for instance).
func (s *Server) handleWorkspaceTree(w http.ResponseWriter, r *http.Request) {
	result, err := s.repos.workspace().Dispatch(r.Context(), "workspace.list", nil)
	if err != nil {
		writeVaultError(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, result)
}
