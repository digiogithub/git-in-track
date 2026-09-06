package server

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/digiogithub/git-in-track/internal/core"
	"github.com/digiogithub/git-in-track/internal/gitops"
	"github.com/digiogithub/git-in-track/internal/vault"
)

// The write half of the team project list (docs/07 section 5.6). Both routes go
// through the workspace and through the very same vault methods the browser
// calls into the WebAssembly module, so the team.yaml a companion writes and
// the one a browser writes are the same bytes.

// teamProjectBody is the request of POST /api/v1/teams/{key}/projects: one
// entry of the `projects:` list of team.yaml, exactly as docs/04 section 3.3
// describes it.
type teamProjectBody struct {
	core.TeamProject
}

// handleTeamProjectAdd serves POST /api/v1/teams/{key}/projects: it declares a
// project repository in a team, which is what makes its items reachable from
// that team's boards and sprints.
//
// A key the team already declares is refused with `team_project_exists` (409),
// because the list is keyed by project key alone (R-PROJ-1).
func (s *Server) handleTeamProjectAdd(w http.ResponseWriter, r *http.Request) {
	var body teamProjectBody
	if !decodeBody(w, r, &body) {
		return
	}
	if strings.TrimSpace(string(body.Key)) == "" {
		failProblem(w, r, codeInvalidRequest, "A project entry needs a `key`.")
		return
	}
	params := vault.TeamProjectAddParams{
		TeamScope: vault.TeamScope{Team: chi.URLParam(r, "key")},
		Project:   body.TeamProject,
	}
	result, err := s.repos.workspace().Dispatch(r.Context(), "team.project.add", mustJSON(params))
	if err != nil {
		writeVaultError(w, r, err)
		return
	}
	s.publishTeamProject(r, result, gitops.ActionCreate)
	writeJSON(w, r, http.StatusCreated, result)
}

// handleTeamProjectRemove serves DELETE /api/v1/teams/{key}/projects/{project}:
// it disconnects a project from a team. Nothing in the project repository is
// touched — the entry is a routing declaration, not the backlog.
//
// A project a board, a sprint or a retro action still references is refused
// with `team_project_referenced` (409) listing what points where; `?force=true`
// accepts leaving those references pointing at an undeclared project.
func (s *Server) handleTeamProjectRemove(w http.ResponseWriter, r *http.Request) {
	force, err := strconv.ParseBool(r.URL.Query().Get("force"))
	if err != nil {
		// An absent or unparsable flag is simply "not forced": the refusal that
		// follows explains what to set.
		force = false
	}
	params := vault.TeamProjectRemoveParams{
		TeamScope: vault.TeamScope{Team: chi.URLParam(r, "key")},
		Key:       chi.URLParam(r, "project"),
		Force:     force,
	}
	result, dispatchErr := s.repos.workspace().Dispatch(r.Context(), "team.project.remove", mustJSON(params))
	if dispatchErr != nil {
		writeVaultError(w, r, dispatchErr)
		return
	}
	s.publishTeamProject(r, result, gitops.ActionDelete)
	writeJSON(w, r, http.StatusOK, result)
}

// publishTeamProject tells the connected UIs that team.yaml changed and hands
// the write to commit-on-save, exactly as a board or a sprint write does.
func (s *Server) publishTeamProject(r *http.Request, result any, action gitops.Action) {
	changed, ok := result.(vault.TeamProjectResult)
	if !ok {
		return
	}
	s.publishWriteSets(r, changed.Writes)
	s.commitWriteSets(r.Context(), changed.Writes,
		sprintFields(string(changed.Project.Key), "team project", action))
}
