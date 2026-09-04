package server

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/digiogithub/git-in-track/internal/core"
)

// The board surface of docs/07 section 9. A board lives in the team repository
// and its cards live in the project repositories, so every route here goes
// through the workspace rather than through one mount: the same call the
// browser makes into the WebAssembly module, over HTTP.

// mountBoards registers the board routes.
func (s *Server) mountBoards(r chi.Router) {
	r.Get("/", s.handleBoardList)
	r.Get("/{slug}", s.handleBoardGet)
	r.Post("/{slug}/cards/move", s.handleBoardCardMove)
	r.Patch("/{slug}", s.handleBoardUpdate)
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
