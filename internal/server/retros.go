package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/digiogithub/git-in-track/internal/core"
	"github.com/digiogithub/git-in-track/internal/gitops"
	"github.com/digiogithub/git-in-track/internal/vault"
)

// The retrospective surface of docs/07 section 5.5. A retro lives in the team
// repository and the tasks its actions were promoted into live in the project
// repositories, so every route here goes through the workspace: the same call
// the browser makes into the WebAssembly module, over HTTP (docs/04 section 9,
// story GIT-US-0027).

// mountRetros registers the retro routes.
func (s *Server) mountRetros(r chi.Router) {
	r.Get("/", s.handleRetroList)
	r.Post("/", s.handleRetroCreate)
	r.Get("/{id}", s.handleRetroGet)
	r.Patch("/{id}", s.handleRetroUpdate)
	r.Post("/{id}/actions/promote", s.handleRetroPromote)
}

// handleRetroList serves GET /api/v1/retros?sprint=&board=&state=. The answer
// carries the still-open actions of the retros it lists, because a team about
// to run a new retro has to see them first (docs/04 section 9.1, step 7).
func (s *Server) handleRetroList(w http.ResponseWriter, r *http.Request) {
	params := map[string]string{
		"sprint": r.URL.Query().Get("sprint"),
		"board":  r.URL.Query().Get("board"),
		"state":  r.URL.Query().Get("state"),
	}
	result, err := s.repos.workspace().Dispatch(r.Context(), "retro.list", mustJSON(params))
	if err != nil {
		writeVaultError(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, result)
}

// handleRetroGet serves GET /api/v1/retros/{id}, with the retro's revision as
// ETag.
func (s *Server) handleRetroGet(w http.ResponseWriter, r *http.Request) {
	result, err := s.repos.workspace().Dispatch(r.Context(), "retro.get",
		mustJSON(map[string]string{"id": chi.URLParam(r, "id")}))
	if err != nil {
		writeVaultError(w, r, err)
		return
	}
	rev := ""
	if view, ok := result.(core.RetroView); ok {
		rev = string(view.Retro.Rev)
	}
	writeEntity(w, r, http.StatusOK, result, rev)
}

// handleRetroCreate serves POST /api/v1/retros. The id is allocated by the core
// from the team key, so the body never carries one.
func (s *Server) handleRetroCreate(w http.ResponseWriter, r *http.Request) {
	var body vault.RetroCreateParams
	if r.ContentLength > 0 && !decodeBody(w, r, &body) {
		return
	}
	result, err := s.repos.workspace().Dispatch(r.Context(), "retro.create", mustJSON(body))
	if err != nil {
		writeVaultError(w, r, err)
		return
	}
	s.publishRetroWrite(r, result, gitops.ActionCreate)
	writeJSON(w, r, http.StatusCreated, result)
}

// handleRetroUpdate serves PATCH /api/v1/retros/{id}: notes added and grouped,
// votes cast, actions selected. Every change is one write to the retro file.
func (s *Server) handleRetroUpdate(w http.ResponseWriter, r *http.Request) {
	rev, ok := requireIfMatch(w, r)
	if !ok {
		return
	}
	var patch vault.RetroPatch
	if !decodeBody(w, r, &patch) {
		return
	}
	params := vault.RetroUpdateParams{ID: chi.URLParam(r, "id"), Rev: rev, Patch: patch}
	result, err := s.repos.workspace().Dispatch(r.Context(), "retro.update", mustJSON(params))
	if err != nil {
		writeVaultError(w, r, err)
		return
	}
	s.publishRetroWrite(r, result, gitops.ActionUpdate)
	writeJSON(w, r, http.StatusOK, result)
}

// handleRetroPromote serves POST /api/v1/retros/{id}/actions/promote. The body
// names the action and the project the task belongs in; the answer carries both
// the created task and the retro that now references it (R-RETRO-2).
func (s *Server) handleRetroPromote(w http.ResponseWriter, r *http.Request) {
	rev, ok := requireIfMatch(w, r)
	if !ok {
		return
	}
	var body struct {
		Action  string   `json:"action"`
		Project string   `json:"project"`
		Labels  []string `json:"labels,omitempty"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	if body.Action == "" || body.Project == "" {
		failProblem(w, r, codeInvalidRequest,
			"Promoting an action needs the `action` id and the `project` the task belongs in.")
		return
	}
	params := vault.RetroPromoteParams{
		ID: chi.URLParam(r, "id"), Action: body.Action,
		Project: body.Project, Labels: body.Labels, Rev: rev,
	}
	result, err := s.repos.workspace().Dispatch(r.Context(), "retro.promote", mustJSON(params))
	if err != nil {
		writeVaultError(w, r, err)
		return
	}
	s.publishRetroWrite(r, result, gitops.ActionUpdate)
	writeJSON(w, r, http.StatusOK, result)
}

// publishRetroWrite announces the files a retro call wrote, so that every
// connected UI reloads the retro and, after a promotion, the project backlog
// the new task landed in.
func (s *Server) publishRetroWrite(r *http.Request, result any, action gitops.Action) {
	written, ok := result.(vault.RetroResult)
	if !ok {
		return
	}
	s.publishWriteSets(r, written.Writes)
	s.commitWriteSets(r.Context(), written.Writes, sprintFields(written.Retro.Retro.ID, "retro", action))
}
