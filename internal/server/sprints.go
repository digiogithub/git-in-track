package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/digiogithub/git-in-track/internal/core"
	"github.com/digiogithub/git-in-track/internal/gitops"
	"github.com/digiogithub/git-in-track/internal/vault"
)

// The sprint surface of docs/07 section 5.5. A sprint lives in the team
// repository and its items live in the project repositories, so every route
// here goes through the workspace: the same call the browser makes into the
// WebAssembly module, over HTTP (docs/04 section 8, story GIT-US-0018).

// mountSprints registers the sprint routes.
func (s *Server) mountSprints(r chi.Router) {
	r.Get("/", s.handleSprintList)
	r.Post("/", s.handleSprintCreate)
	r.Get("/{id}", s.handleSprintGet)
	r.Patch("/{id}", s.handleSprintUpdate)
	r.Post("/{id}/start", s.handleSprintStart)
	r.Post("/{id}/close", s.handleSprintClose)
	r.Get("/{id}/burndown", s.notImplemented("Sprint burndown arrives with the metrics of GIT-US-0028."))
}

// handleSprintList serves GET /api/v1/sprints?board=&state=.
func (s *Server) handleSprintList(w http.ResponseWriter, r *http.Request) {
	params := map[string]string{
		"board": r.URL.Query().Get("board"),
		"state": r.URL.Query().Get("state"),
	}
	result, err := s.repos.workspace().Dispatch(r.Context(), "sprint.list", mustJSON(params))
	if err != nil {
		writeVaultError(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, result)
}

// handleSprintGet serves GET /api/v1/sprints/{id}: the scope, the candidates
// the board would offer and the metrics, with the sprint's revision as ETag.
func (s *Server) handleSprintGet(w http.ResponseWriter, r *http.Request) {
	result, err := s.repos.workspace().Dispatch(r.Context(), "sprint.get",
		mustJSON(map[string]string{"id": chi.URLParam(r, "id")}))
	if err != nil {
		writeVaultError(w, r, err)
		return
	}
	rev := ""
	if view, ok := result.(core.SprintView); ok {
		rev = string(view.Sprint.Rev)
	}
	writeEntity(w, r, http.StatusOK, result, rev)
}

// handleSprintCreate serves POST /api/v1/sprints. The id is allocated by the
// core from the team key and the sprints already on disk, so the body never
// carries one.
func (s *Server) handleSprintCreate(w http.ResponseWriter, r *http.Request) {
	var body vault.SprintCreateParams
	if !decodeBody(w, r, &body) {
		return
	}
	if body.Board == "" {
		failProblem(w, r, codeInvalidRequest, "A sprint needs the `board` it belongs to.")
		return
	}
	if body.Start == "" || body.End == "" {
		failProblem(w, r, codeInvalidRequest, "A sprint needs a `start` and an `end` date.")
		return
	}
	result, err := s.repos.workspace().Dispatch(r.Context(), "sprint.create", mustJSON(body))
	if err != nil {
		writeVaultError(w, r, err)
		return
	}
	s.publishSprintWrite(r, result)
	writeJSON(w, r, http.StatusCreated, result)
}

// handleSprintUpdate serves PATCH /api/v1/sprints/{id}: the goal, the dates and
// the scope. Every change is one write to the sprint file in the team
// repository, and never a write to an item (docs/04 R-SPR-2).
func (s *Server) handleSprintUpdate(w http.ResponseWriter, r *http.Request) {
	rev, ok := requireIfMatch(w, r)
	if !ok {
		return
	}
	var patch vault.SprintPatch
	if !decodeBody(w, r, &patch) {
		return
	}
	params := vault.SprintUpdateParams{ID: chi.URLParam(r, "id"), Rev: rev, Patch: patch}
	result, err := s.repos.workspace().Dispatch(r.Context(), "sprint.update", mustJSON(params))
	if err != nil {
		writeVaultError(w, r, err)
		return
	}
	s.publishSprintWrite(r, result)
	writeJSON(w, r, http.StatusOK, result)
}

// handleSprintStart serves POST /api/v1/sprints/{id}/start.
func (s *Server) handleSprintStart(w http.ResponseWriter, r *http.Request) {
	rev, ok := requireIfMatch(w, r)
	if !ok {
		return
	}
	var body struct {
		Force bool `json:"force,omitempty"`
	}
	if r.ContentLength > 0 && !decodeBody(w, r, &body) {
		return
	}
	params := vault.SprintStartParams{ID: chi.URLParam(r, "id"), Rev: rev, Force: body.Force}
	result, err := s.repos.workspace().Dispatch(r.Context(), "sprint.start", mustJSON(params))
	if err != nil {
		writeVaultError(w, r, err)
		return
	}
	s.publishSprintWrite(r, result)
	writeJSON(w, r, http.StatusOK, result)
}

// handleSprintClose serves POST /api/v1/sprints/{id}/close. The body carries
// one decision per unfinished item; an item nobody decided about is left
// exactly where it is (docs/04 R-SPR-3).
func (s *Server) handleSprintClose(w http.ResponseWriter, r *http.Request) {
	rev, ok := requireIfMatch(w, r)
	if !ok {
		return
	}
	var body struct {
		Carry []vault.SprintCarry `json:"carry,omitempty"`
	}
	if r.ContentLength > 0 && !decodeBody(w, r, &body) {
		return
	}
	params := vault.SprintCloseParams{ID: chi.URLParam(r, "id"), Rev: rev, Carry: body.Carry}
	result, err := s.repos.workspace().Dispatch(r.Context(), "sprint.close", mustJSON(params))
	if err != nil {
		writeVaultError(w, r, err)
		return
	}
	s.publishSprintWrite(r, result)
	writeJSON(w, r, http.StatusOK, result)
}

// handleBoardUpdate serves PATCH /api/v1/boards/{slug}: the columns, their WIP
// limits, the filters and — on a scrum board — the sprint it is scoped to. The
// card order is never patched here; it moves one card at a time.
func (s *Server) handleBoardUpdate(w http.ResponseWriter, r *http.Request) {
	rev, ok := requireIfMatch(w, r)
	if !ok {
		return
	}
	var patch vault.BoardPatch
	if !decodeBody(w, r, &patch) {
		return
	}
	params := vault.BoardUpdateParams{Board: chi.URLParam(r, "slug"), Rev: rev, Patch: patch}
	result, err := s.repos.workspace().Dispatch(r.Context(), "board.update", mustJSON(params))
	if err != nil {
		writeVaultError(w, r, err)
		return
	}
	if updated, ok := result.(vault.BoardUpdateResult); ok {
		s.publishWriteSets(r, updated.Writes)
		s.commitWriteSets(r.Context(), updated.Writes, sprintFields(updated.Board.ID, "board", gitops.ActionUpdate))
		writeEntity(w, r, http.StatusOK, result, string(updated.Board.Rev))
		return
	}
	writeJSON(w, r, http.StatusOK, result)
}

// publishSprintWrite announces the files a sprint call wrote, so that every
// connected UI reloads the board and the items behind it.
func (s *Server) publishSprintWrite(r *http.Request, result any) {
	if written, ok := result.(vault.SprintResult); ok {
		s.publishWriteSets(r, written.Writes)
		s.commitWriteSets(r.Context(), written.Writes, sprintFields(written.Sprint.Sprint.ID, "sprint", gitops.ActionUpdate))
	}
}
