package server

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/digiogithub/git-in-track/internal/core"
	"github.com/digiogithub/git-in-track/internal/gitops"
	"github.com/digiogithub/git-in-track/internal/vault"
)

// The board surface of docs/07 section 9. A board lives in the team repository
// and its cards live in the project repositories, so every route here goes
// through the workspace rather than through one mount: the same call the
// browser makes into the WebAssembly module, over HTTP.

// mountBoards registers the board routes.
func (s *Server) mountBoards(r chi.Router) {
	r.Get("/", s.handleBoardList)
	r.Post("/", s.handleBoardCreate)
	r.Get("/{slug}", s.handleBoardGet)
	r.Post("/{slug}/cards/move", s.handleBoardCardMove)
	r.Patch("/{slug}", s.handleBoardUpdate)
	r.Delete("/{slug}", s.handleBoardDelete)
}

// handleBoardCreate serves POST /api/v1/boards: one new board file in the team
// repository and nothing else. A board is a view, so creating one adds no item
// anywhere; the cards it shows are the ones its scope and its filters select.
//
// There is nothing yet to conflict with, so the call carries no If-Match; a
// slug that is already a board is refused rather than overwritten.
func (s *Server) handleBoardCreate(w http.ResponseWriter, r *http.Request) {
	var params vault.BoardCreateParams
	if !decodeBody(w, r, &params) {
		return
	}
	if params.Title == "" {
		failProblem(w, r, codeInvalidRequest, "A board needs a `title`.")
		return
	}
	result, err := s.repos.workspace().Dispatch(r.Context(), "board.create", mustJSON(params))
	if err != nil {
		writeVaultError(w, r, err)
		return
	}
	if created, ok := result.(vault.BoardCreateResult); ok {
		s.publishWriteSets(r, created.Writes)
		s.commitWriteSets(r.Context(), created.Writes,
			sprintFields(created.Board.ID, "board", gitops.ActionCreate))
		writeEntity(w, r, http.StatusCreated, result, string(created.Board.Rev))
		return
	}
	writeJSON(w, r, http.StatusCreated, result)
}

// handleBoardDelete serves DELETE /api/v1/boards/{slug}. It removes a view and
// nothing else: every item the board's cards referenced lives in its own
// project repository and is untouched. A board a sprint belongs to is refused.
func (s *Server) handleBoardDelete(w http.ResponseWriter, r *http.Request) {
	rev, ok := requireIfMatch(w, r)
	if !ok {
		return
	}
	params := vault.BoardDeleteParams{Board: chi.URLParam(r, "slug"), Rev: rev}
	result, err := s.repos.workspace().Dispatch(r.Context(), "board.delete", mustJSON(params))
	if err != nil {
		writeVaultError(w, r, err)
		return
	}
	if deleted, ok := result.(vault.BoardDeleteResult); ok {
		s.publishWriteSets(r, deleted.Writes)
		s.commitWriteSets(r.Context(), deleted.Writes,
			sprintFields(deleted.Board, "board", gitops.ActionDelete))
	}
	writeJSON(w, r, http.StatusOK, result)
}

// handleBoardList serves GET /api/v1/boards.
func (s *Server) handleBoardList(w http.ResponseWriter, r *http.Request) {
	result, err := s.repos.workspace().Dispatch(r.Context(), "board.list", nil)
	if err != nil {
		writeVaultError(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, result)
}

// handleBoardGet serves GET /api/v1/boards/{slug}. The board is always resolved
// against the repositories that are open: a card whose project nobody cloned
// comes back marked remote rather than missing (docs/04 section 7).
func (s *Server) handleBoardGet(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	result, err := s.repos.workspace().Dispatch(r.Context(), "board.get",
		mustJSON(map[string]string{"board": slug}))
	if err != nil {
		writeVaultError(w, r, err)
		return
	}
	rev := ""
	if view, ok := result.(core.BoardView); ok {
		rev = string(view.Rev)
	}
	writeEntity(w, r, http.StatusOK, result, rev)
}

// handleBoardCardMove serves POST /api/v1/boards/{slug}/cards/move.
//
// If-Match carries the revision of the board the client read; `itemRev` in the
// body carries the revision of the item, because the two live in different
// repositories and therefore have two independent optimistic locks.
func (s *Server) handleBoardCardMove(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	rev, ok := requireIfMatch(w, r)
	if !ok {
		return
	}
	var body struct {
		Ref      string `json:"ref"`
		ToColumn string `json:"toColumn"`
		Position *int   `json:"position,omitempty"`
		Status   string `json:"status,omitempty"`
		ItemRev  string `json:"itemRev,omitempty"`
		Force    bool   `json:"force,omitempty"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	if body.Ref == "" || body.ToColumn == "" {
		failProblem(w, r, codeInvalidRequest, "A card move needs a `ref` and a `toColumn`.")
		return
	}
	position := -1
	if body.Position != nil {
		position = *body.Position
	}
	// `?force=true` is accepted as well, so that a scripted retry after a WIP
	// refusal is one flag on the URL.
	force := body.Force
	if raw := r.URL.Query().Get("force"); raw != "" {
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			failProblem(w, r, codeInvalidRequest, "`force` must be a boolean.")
			return
		}
		force = force || parsed
	}

	params := map[string]any{
		"board": slug, "ref": body.Ref, "toColumn": body.ToColumn,
		"position": position, "status": body.Status,
		"rev": rev, "itemRev": body.ItemRev, "force": force,
	}
	result, err := s.repos.workspace().Dispatch(r.Context(), "board.move", mustJSON(params))
	if err != nil {
		writeVaultError(w, r, err)
		return
	}
	s.publishBoardMove(r, result)
	writeJSON(w, r, http.StatusOK, result)
}
