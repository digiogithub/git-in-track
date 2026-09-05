package server

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/digiogithub/git-in-track/internal/core"
)

// handleTeams serves GET /api/v1/teams: every mounted repository holding a
// team.yaml, in mount order. A workspace may hold several since GIT-US-0036,
// and the list is returned even when it is empty, because the client must be
// able to tell "no team repository is open" from an error.
func (s *Server) handleTeams(w http.ResponseWriter, r *http.Request) {
	result, err := s.repos.workspace().Dispatch(r.Context(), "team.list", nil)
	if err != nil {
		writeVaultError(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, result)
}

// handleTeam serves GET /api/v1/teams/{key}, where {key} is the `key:` of a
// team.yaml or the id of the repository holding it. It resolves that key
// against every mounted team rather than against the first one.
func (s *Server) handleTeam(w http.ResponseWriter, r *http.Request) {
	key := chi.URLParam(r, "key")
	summary, err := s.repos.workspace().Dispatch(r.Context(), "team.get",
		mustJSON(map[string]string{"team": key}))
	if err != nil {
		writeVaultError(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, summary)
}

// teamOf reads the team a request acts on from `?team=`. Every team-scoped
// route accepts it, and it takes either the `key:` of a team.yaml or the id of
// the repository holding it. It is optional while the workspace holds a single
// team, and required as soon as it holds two (docs/07 section 5.5).
func teamOf(r *http.Request) string {
	return r.URL.Query().Get("team")
}

// teamFallback lets a request body name the team instead of the query string,
// which is what a POST whose params struct already carries the scope does.
func teamFallback(r *http.Request, body string) string {
	if body != "" {
		return body
	}
	return teamOf(r)
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

// handleSnapshotList serves GET /api/v1/snapshots: the committed index snapshot
// of every project team.yaml declares, with its age and its staleness.
func (s *Server) handleSnapshotList(w http.ResponseWriter, r *http.Request) {
	result, err := s.repos.workspace().Dispatch(r.Context(), "snapshot.list",
		mustJSON(map[string]string{"team": teamOf(r)}))
	if err != nil {
		writeVaultError(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, result)
}

// handleSnapshotRefresh serves POST /api/v1/snapshots: regenerate the snapshots
// of the projects this machine has cloned and write the ones that changed into
// the team repository. It is the companion-side form of `gintrack snapshot`.
func (s *Server) handleSnapshotRefresh(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Projects      []string `json:"projects,omitempty"`
		GeneratedBy   string   `json:"generatedBy,omitempty"`
		IncludeClosed *bool    `json:"includeClosed,omitempty"`
		DryRun        bool     `json:"dryRun,omitempty"`
		Team          string   `json:"team,omitempty"`
	}
	if r.ContentLength > 0 && !decodeBody(w, r, &body) {
		return
	}
	body.Team = teamFallback(r, body.Team)
	result, err := s.repos.workspace().Dispatch(r.Context(), "snapshot.refresh", mustJSON(body))
	if err != nil {
		writeVaultError(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, result)
}
