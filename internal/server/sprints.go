package server

import (
	"net/http"
	"strings"

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
	// Moving the unfinished work of a sprint without closing either sprint
	// (GIT-US-0085). It is a separate route because it is a separate decision:
	// a close grades a sprint, a transfer only moves scope.
	r.Post("/{id}/transfer", s.handleSprintTransfer)
	r.Get("/{id}/burndown", s.handleSprintMetrics)
}

// handleSprintList serves GET /api/v1/sprints?board=&state=.
func (s *Server) handleSprintList(w http.ResponseWriter, r *http.Request) {
	params := map[string]string{
		"board": r.URL.Query().Get("board"),
		"state": r.URL.Query().Get("state"),
		"team":  teamOf(r),
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
		mustJSON(map[string]string{"id": chi.URLParam(r, "id"), "team": teamOf(r)}))
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

// handleSprintMetrics serves GET /api/v1/sprints/{id}/burndown: the burndown,
// the cumulative flow diagram and the flow statistics of one sprint, together
// with the provenance of the history they were reconstructed from
// (docs/04 section 12, docs/07 section 5.5).
//
// The three come back together because they are one reconstruction of one
// window: splitting them across routes would walk the history twice.
func (s *Server) handleSprintMetrics(w http.ResponseWriter, r *http.Request) {
	result, err := s.repos.workspace().Dispatch(r.Context(), "sprint.metrics",
		mustJSON(map[string]string{"id": chi.URLParam(r, "id"), "team": teamOf(r)}))
	if err != nil {
		writeVaultError(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, result)
}

// handleSprintCreate serves POST /api/v1/sprints. The id is allocated by the
// core from the team key and the sprints already on disk, so the body never
// carries one.
func (s *Server) handleSprintCreate(w http.ResponseWriter, r *http.Request) {
	var body vault.SprintCreateParams
	if !decodeBody(w, r, &body) {
		return
	}
	body.Team = teamFallback(r, body.Team)
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
	params := vault.SprintUpdateParams{
		TeamScope: vault.TeamScope{Team: teamOf(r)},
		ID:        chi.URLParam(r, "id"), Rev: rev, Patch: patch,
	}
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
	params := vault.SprintStartParams{
		TeamScope: vault.TeamScope{Team: teamOf(r)},
		ID:        chi.URLParam(r, "id"), Rev: rev, Force: body.Force,
	}
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
		Carry    []vault.SprintCarry   `json:"carry,omitempty"`
		Transfer *vault.SprintTransfer `json:"transfer,omitempty"`
		DryRun   bool                  `json:"dryRun,omitempty"`
	}
	if r.ContentLength > 0 && !decodeBody(w, r, &body) {
		return
	}
	params := vault.SprintCloseParams{
		TeamScope: vault.TeamScope{Team: teamOf(r)},
		ID:        chi.URLParam(r, "id"), Rev: rev, Carry: body.Carry,
		Transfer: body.Transfer, DryRun: body.DryRun,
	}
	result, err := s.repos.workspace().Dispatch(r.Context(), "sprint.close", mustJSON(params))
	if err != nil {
		writeVaultError(w, r, err)
		return
	}
	s.publishSprintWrite(r, result)
	writeJSON(w, r, http.StatusOK, result)
}

// handleSprintTransfer serves POST /api/v1/sprints/{id}/transfer: move the
// unfinished references of one sprint into another sprint or back to their
// project backlogs, without closing anything.
//
// `mode` defaults to `next`, and `carry` overrides it for the references it
// names. A per-item failure is not an error: it comes back on its own
// `report.carried[].error` line with a 200, because the rest of the transfer
// still happened (docs/04 R-SPR-8).
func (s *Server) handleSprintTransfer(w http.ResponseWriter, r *http.Request) {
	rev, ok := requireIfMatch(w, r)
	if !ok {
		return
	}
	var body struct {
		Mode   string              `json:"mode,omitempty"`
		Target string              `json:"target,omitempty"`
		Carry  []vault.SprintCarry `json:"carry,omitempty"`
		DryRun bool                `json:"dryRun,omitempty"`
	}
	if r.ContentLength > 0 && !decodeBody(w, r, &body) {
		return
	}
	params := vault.SprintTransferParams{
		TeamScope: vault.TeamScope{Team: teamOf(r)},
		ID:        chi.URLParam(r, "id"), Rev: rev,
		Mode: body.Mode, Target: body.Target, Carry: body.Carry, DryRun: body.DryRun,
	}
	result, err := s.repos.workspace().Dispatch(r.Context(), "sprint.transfer", mustJSON(params))
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
	params := vault.BoardUpdateParams{
		TeamScope: vault.TeamScope{Team: teamOf(r)},
		Board:     chi.URLParam(r, "slug"), Rev: rev, Patch: patch,
	}
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
//
// A dry run publishes nothing at all — no file event, no commit, no
// `sprint.changed`, no `item.changed`. The gate is `!DryRun` rather than "the
// write set is empty" on purpose: a preview that happened to write nothing and
// a commitment that happened to write nothing are different things, and only
// the flag says which one this was (R-SPR-3).
func (s *Server) publishSprintWrite(r *http.Request, result any) {
	written, ok := result.(vault.SprintResult)
	if !ok {
		return
	}
	if written.DryRun {
		return
	}
	s.publishWriteSets(r, written.Writes)
	s.commitWriteSets(r.Context(), written.Writes, sprintFields(written.Sprint.Sprint.ID, "sprint", gitops.ActionUpdate))
	s.publishSprintChanged(r, written)
}

// sprintChangedData is the payload of a `sprint.changed` event: which sprint
// moved, on which board, and what its close or transfer did to the scope.
type sprintChangedData struct {
	Sprint string `json:"sprint"`
	Board  string `json:"board"`
	State  string `json:"state"`
	// Carried is how many references were moved, and Failed how many decisions
	// could not be applied — a project this machine has not cloned is the
	// common one (R-SPR-8).
	Carried   int    `json:"carried"`
	Failed    int    `json:"failed"`
	Origin    string `json:"origin"`
	RequestID string `json:"requestId,omitempty"`
}

// publishSprintChanged announces a sprint whose scope moved, and one
// `item.changed` per reference that actually moved, so that a backlog view open
// in another tab refreshes the items and not only the board.
func (s *Server) publishSprintChanged(r *http.Request, written vault.SprintResult) {
	requestID := requestIDOf(r)
	data := sprintChangedData{
		Sprint: written.Sprint.Sprint.ID, Board: written.Sprint.Sprint.Board,
		State: string(written.Sprint.Sprint.State), Origin: "api", RequestID: requestID,
	}
	if report := written.Report; report != nil {
		for _, carried := range report.Carried {
			if carried.Error != "" {
				data.Failed++
				continue
			}
			data.Carried++
			s.publishCarriedItem(carried.Ref, requestID)
		}
	}
	s.hub.Publish(eventSprintChanged, data)
}

// publishCarriedItem announces one item a transfer moved. A reference names the
// project it belongs to, so the event can say which repository changed; a
// reference whose project this machine has not cloned is skipped, because there
// is no local item to refresh.
func (s *Server) publishCarriedItem(ref, requestID string) {
	project, id, found := strings.Cut(ref, "/")
	if !found {
		project, id = "", ref
	}
	m, ok := s.repos.forProject(project)
	if !ok {
		return
	}
	s.hub.Publish(eventItemChanged, itemChangedData{
		Repo: m.id, ID: id, Op: "updated", Origin: "api", RequestID: requestID,
	})
}
