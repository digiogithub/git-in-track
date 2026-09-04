package vault

import (
	"context"
	"fmt"
	"time"

	"github.com/digiogithub/git-in-track/internal/core"
)

// This file is the board half of the CoreApi contract: "board.list",
// "board.get" and "board.move". A board lives in the team repository and its
// cards live in the project repositories, so the three methods are answered by
// the workspace; a lone vault answers for what it can see itself, exactly as it
// does for "team.get".
//
// Every decision — which column a card belongs to, which status a move implies,
// whether a WIP limit is exceeded — is taken by internal/core. What is here is
// plumbing: read the files, hand the pieces to the core, write the answer back.

// WipCode is the machine code of a move that would put a column over its WIP
// limit. The limit is advisory (docs/04 R-COL-5), so the caller may repeat the
// call with `force` — but never by accident: it has to say so.
const WipCode = "wip_limit_exceeded"

// RemoteCardCode is the machine code of a move on a card whose project nobody
// cloned (docs/04 section 7).
const RemoteCardCode = "repo_not_cloned"

// BoardSummary is one entry of "board.list": enough to render the board index
// without resolving a single card.
type BoardSummary struct {
	ID          string            `json:"id"`
	Kind        core.BoardKind    `json:"kind"`
	Title       string            `json:"title"`
	Description string            `json:"description,omitempty"`
	Path        string            `json:"path"`
	Rev         string            `json:"rev"`
	VaultID     string            `json:"vaultId,omitempty"`
	Projects    []core.ProjectKey `json:"projects"`
	Columns     int               `json:"columns"`
	Sprint      string            `json:"sprint,omitempty"`
	Diagnostics []core.Diagnostic `json:"diagnostics"`
}

// BoardListResult is the answer of "board.list".
type BoardListResult struct {
	Boards      []BoardSummary    `json:"boards"`
	Diagnostics []core.Diagnostic `json:"diagnostics"`
}

// BoardMoveParams is the input of "board.move".
type BoardMoveParams struct {
	Board string `json:"board"`
	Ref   string `json:"ref"`
	// ToColumn is the target column id.
	ToColumn string `json:"toColumn"`
	// Position is the 0-based index in the target column; -1 appends.
	Position int `json:"position"`
	// Status overrides the status the column mapping would pick, for a column
	// that maps several.
	Status string `json:"status,omitempty"`
	// Rev is the board revision the caller read. Empty skips the check.
	Rev string `json:"rev,omitempty"`
	// ItemRev is the item revision the caller read. Empty skips the check.
	ItemRev string `json:"itemRev,omitempty"`
	// Force confirms a move over a WIP limit, and a transition the project
	// workflow does not declare.
	Force bool `json:"force,omitempty"`
}

// WipStatus is the live WIP condition of the target column after a move.
type WipStatus struct {
	Column   string `json:"column"`
	Used     int    `json:"used"`
	Limit    int    `json:"limit"`
	Exceeded bool   `json:"exceeded"`
}

// MovePlan is what a move implied, echoed back so that the UI can explain what
// happened without recomputing it.
type MovePlan struct {
	Ref           string    `json:"ref"`
	FromColumn    string    `json:"fromColumn,omitempty"`
	ToColumn      string    `json:"toColumn"`
	Status        string    `json:"status,omitempty"`
	StatusChanged bool      `json:"statusChanged"`
	Choices       []string  `json:"choices,omitempty"`
	WIP           WipStatus `json:"wip"`
	// Sprint is the sprint a scrum board is scoped to, and SprintAdd reports
	// that the move added the reference to it (docs/04 section 5.5).
	Sprint    string `json:"sprint,omitempty"`
	SprintAdd bool   `json:"sprintAdd,omitempty"`
}

// RepoWriteSet is a WriteSet plus the repository it belongs to. A move writes
// to two repositories — the item in its project clone, the order list in the
// team repository — and the host has to persist each in the right place
// (docs/04 R-MOVE-1).
type RepoWriteSet struct {
	VaultID string   `json:"vaultId"`
	Written []File   `json:"written"`
	Removed []string `json:"removed"`
}

// BoardMoveResult is the answer of "board.move".
type BoardMoveResult struct {
	Board  core.BoardView `json:"board"`
	Item   *core.Item     `json:"item,omitempty"`
	Move   MovePlan       `json:"move"`
	Writes []RepoWriteSet `json:"writes"`
}

// BoardParams is the input of "board.get".
type BoardParams struct {
	Board string `json:"board"`
}

// ------------------------------------------------------------------ vault ---

// boardStore returns the store over this vault's `.pmngr/boards/`. The caller
// holds the lock.
func (v *Vault) boardStore() (*core.BoardStore, error) {
	if v.team == nil {
		return nil, failf("not_found", "this repository has no %s at its root", core.TeamFileName)
	}
	store := core.NewBoardStore(v.fs, v.team.TeamDirPath)
	if v.now != nil {
		store.Clock = core.ClockFunc(v.now)
	}
	return store, nil
}

// Boards returns every board of a team repository, with the diagnostics of the
// files that could not be parsed. It takes the vault lock.
func (v *Vault) Boards(ctx context.Context) ([]*core.Board, []core.Diagnostic, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.boards(ctx)
}

func (v *Vault) boards(ctx context.Context) ([]*core.Board, []core.Diagnostic, error) {
	store, err := v.boardStore()
	if err != nil {
		return nil, nil, err
	}
	boards, diags, err := store.List(ctx)
	if err != nil {
		return nil, nil, failf("internal", "%v", err)
	}
	return boards, diags, nil
}

// Board reads one board by its slug. It takes the vault lock.
func (v *Vault) Board(ctx context.Context, id string) (*core.Board, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.board(ctx, id)
}

func (v *Vault) board(ctx context.Context, id string) (*core.Board, error) {
	store, err := v.boardStore()
	if err != nil {
		return nil, err
	}
	board, err := store.Get(ctx, id)
	if err != nil {
		return nil, failf("not_found", "no board %q in %s", id, store.Dir())
	}
	return board, nil
}

// WriteBoard persists a board and reports what the host must save. It takes the
// vault lock.
func (v *Vault) WriteBoard(ctx context.Context, b *core.Board, expected core.Rev) (*core.Board, WriteSet, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	store, err := v.boardStore()
	if err != nil {
		return nil, WriteSet{}, err
	}
	v.fs.begin()
	written, err := store.Write(ctx, b, expected)
	if err != nil {
		return nil, WriteSet{}, fmt.Errorf("write board: %w", err)
	}
	writes, err := v.commit(ctx)
	if err != nil {
		return nil, WriteSet{}, err
	}
	return written, writes, nil
}

// BoardSource collects the items of one project the vault serves, so that a
// board can be rendered over them. It takes the vault lock.
func (v *Vault) BoardSource(ctx context.Context, key core.ProjectKey, vaultID string) (core.BoardSource, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.boardSource(ctx, key, vaultID)
}

// boardSource is BoardSource with the lock already held. Bodies are dropped:
// a board renders hundreds of cards and none of them shows one.
func (v *Vault) boardSource(ctx context.Context, key core.ProjectKey, vaultID string) (core.BoardSource, bool) {
	var ref core.ProjectRef
	found := false
	for _, p := range v.projects {
		if !p.Team && p.Key == key {
			ref, found = p, true
			break
		}
	}
	if !found {
		return core.BoardSource{}, false
	}
	source := core.BoardSource{Project: key, VaultID: vaultID, Config: ref.Config}
	filter := core.Filter{Projects: []core.ProjectKey{key}, Limit: core.MaxLimit, Sort: "id"}
	for {
		page, err := v.index.Items(ctx, filter)
		if err != nil {
			break
		}
		for i := range page.Items {
			page.Items[i].Body = ""
		}
		source.Items = append(source.Items, page.Items...)
		if page.NextCursor == "" {
			break
		}
		filter.Cursor = page.NextCursor
	}
	return source, true
}

// MoveItemStatus writes the status of one item and reports what changed. It is
// the project-repository half of a card move, and it takes the vault lock.
func (v *Vault) MoveItemStatus(
	ctx context.Context, id core.ItemID, status core.Status, expected core.Rev, force bool,
) (*core.Item, WriteSet, error) {
	v.mu.Lock()
	defer v.mu.Unlock()

	store, err := v.storeForItem(id)
	if err != nil {
		return nil, WriteSet{}, err
	}
	v.fs.begin()
	it, err := store.MoveWith(ctx, id, status, expected, core.MoveOptions{Force: force})
	if err != nil {
		return nil, WriteSet{}, fmt.Errorf("move %s: %w", id, err)
	}
	writes, err := v.commit(ctx)
	if err != nil {
		return nil, WriteSet{}, err
	}
	return it, writes, nil
}

// ProjectConfig returns the workflow of a project the vault serves. It takes
// the vault lock.
func (v *Vault) ProjectConfig(key core.ProjectKey) *core.ProjectConfig {
	v.mu.Lock()
	defer v.mu.Unlock()
	for _, p := range v.projects {
		if !p.Team && p.Key == key {
			return p.Config
		}
	}
	return nil
}

// boardList answers "board.list" for a single vault.
func (v *Vault) boardList(ctx context.Context) (any, error) {
	boards, diags, err := v.boards(ctx)
	if err != nil {
		return nil, err
	}
	declared, configs := v.teamScope()
	out := BoardListResult{Boards: []BoardSummary{}, Diagnostics: diags}
	if out.Diagnostics == nil {
		out.Diagnostics = []core.Diagnostic{}
	}
	for _, b := range boards {
		out.Boards = append(out.Boards, summarizeBoard(b, "", declared, configs))
	}
	return out, nil
}

// boardGet answers "board.get" for a single vault: the board rendered over the
// projects this very repository serves. A workspace renders the same board over
// every open repository.
func (v *Vault) boardGet(ctx context.Context, raw []byte) (any, error) {
	p, err := decodeParams[BoardParams](raw)
	if err != nil {
		return nil, err
	}
	board, err := v.board(ctx, p.Board)
	if err != nil {
		return nil, err
	}
	declared, configs := v.teamScope()
	snapshots := v.snapshots()
	input := core.BoardInput{Declared: declared, Snapshots: snapshots}
	if v.team != nil && v.team.Config != nil {
		input.Projects = append(input.Projects, v.team.Config.Projects...)
	}
	for _, key := range declared {
		if source, ok := v.boardSource(ctx, key, ""); ok {
			input.Sources = append(input.Sources, source)
		}
	}
	view := core.BuildBoardView(board, input)
	view.Diagnostics = append(view.Diagnostics, board.Validate(declared, configs)...)
	view.Diagnostics = append(view.Diagnostics, snapshots.Diagnostics()...)
	return view, nil
}

// teamScope returns the project keys team.yaml declares and the workflows of
// the ones this vault serves. The caller holds the lock.
func (v *Vault) teamScope() (declared []core.ProjectKey, configs map[core.ProjectKey]*core.ProjectConfig) {
	if v.team != nil && v.team.Config != nil {
		for _, p := range v.team.Config.Projects {
			declared = append(declared, p.Key)
		}
	}
	configs = map[core.ProjectKey]*core.ProjectConfig{}
	for _, p := range v.projects {
		if !p.Team && p.Config != nil {
			configs[p.Key] = p.Config
		}
	}
	return declared, configs
}

// summarizeBoard renders one board index entry.
func summarizeBoard(
	b *core.Board, vaultID string,
	declared []core.ProjectKey, configs map[core.ProjectKey]*core.ProjectConfig,
) BoardSummary {
	diags := b.Validate(declared, configs)
	if diags == nil {
		diags = []core.Diagnostic{}
	}
	return BoardSummary{
		ID: b.ID, Kind: b.Kind, Title: b.Title, Description: b.Description,
		Path: b.Path, Rev: string(b.Rev), VaultID: vaultID,
		Projects: b.Scope(declared), Columns: len(b.Columns), Sprint: b.Sprint,
		Diagnostics: diags,
	}
}

// -------------------------------------------------------------- workspace ---

// boardContext is everything a workspace-wide board call needs: the repository
// holding the boards, the projects team.yaml declares and the ones an open
// repository actually serves.
type boardContext struct {
	team     *Mount
	declared []core.ProjectKey
	configs  map[core.ProjectKey]*core.ProjectConfig
	owners   map[core.ProjectKey]*Mount
	// projects are the team.yaml declarations, which is what a link to a
	// remote item's file is built from (docs/04 section 7.3).
	projects []core.TeamProject
	// snapshots resolve the cards of the projects nobody cloned.
	snapshots *core.SnapshotSet
	// now is the clock a sprint's remaining days are counted from.
	now time.Time
}

// boardContext gathers the repositories a board is rendered over.
func (w *Workspace) boardContext() (boardContext, error) {
	m, ok := w.TeamMount()
	if !ok {
		return boardContext{}, failf("not_found", "no open repository holds a %s", core.TeamFileName)
	}
	out := boardContext{
		team:    m,
		configs: map[core.ProjectKey]*core.ProjectConfig{},
		owners:  map[core.ProjectKey]*Mount{},
	}
	team := m.Vault.Team()
	if team != nil && team.Config != nil {
		for _, p := range team.Config.Projects {
			out.declared = append(out.declared, p.Key)
		}
	}
	out.projects = m.Vault.TeamProjects()
	out.snapshots = m.Vault.Snapshots()
	out.now = m.Vault.Now()
	for _, key := range out.declared {
		owner, ok := w.MountForProject(key)
		if !ok {
			continue
		}
		out.owners[key] = owner
		if cfg := owner.Vault.ProjectConfig(key); cfg != nil {
			out.configs[key] = cfg
		}
	}
	return out, nil
}

// input builds the render input of a board: one source per cloned project.
func (c boardContext) input(ctx context.Context) core.BoardInput {
	in := core.BoardInput{
		Declared: c.declared, TeamVaultID: c.team.ID,
		Projects: c.projects, Snapshots: c.snapshots, Now: c.now,
	}
	for _, key := range c.declared {
		owner, ok := c.owners[key]
		if !ok {
			continue
		}
		if source, ok := owner.Vault.BoardSource(ctx, key, owner.ID); ok {
			in.Sources = append(in.Sources, source)
		}
	}
	return in
}

// Boards lists every board of the team repository.
func (w *Workspace) Boards(ctx context.Context) (BoardListResult, error) {
	c, err := w.boardContext()
	if err != nil {
		return BoardListResult{}, err
	}
	boards, diags, err := c.team.Vault.Boards(ctx)
	if err != nil {
		return BoardListResult{}, err
	}
	out := BoardListResult{Boards: []BoardSummary{}, Diagnostics: diags}
	if out.Diagnostics == nil {
		out.Diagnostics = []core.Diagnostic{}
	}
	for _, b := range boards {
		out.Boards = append(out.Boards, summarizeBoard(b, c.team.ID, c.declared, c.configs))
	}
	return out, nil
}

// BoardView renders one board over every open repository.
func (w *Workspace) BoardView(ctx context.Context, id string) (core.BoardView, error) {
	c, err := w.boardContext()
	if err != nil {
		return core.BoardView{}, err
	}
	board, err := c.team.Vault.Board(ctx, id)
	if err != nil {
		return core.BoardView{}, err
	}
	return c.render(ctx, board), nil
}

// inputFor is the render input of one board: the sources of input(), plus the
// sprint a scrum board is scoped to (docs/04 section 5.5). A sprint file that
// cannot be read leaves the board unscoped and is reported as a diagnostic by
// render, never as a failure.
func (c boardContext) inputFor(ctx context.Context, board *core.Board) core.BoardInput {
	in := c.input(ctx)
	if board.Kind == core.BoardScrum && board.Sprint != "" {
		if sprint, err := c.team.Vault.Sprint(ctx, board.Sprint); err == nil {
			in.Sprint = sprint
		}
	}
	return in
}

// render builds the view of an already loaded board and appends the findings of
// its validation, so that the UI shows a broken column instead of hiding it.
func (c boardContext) render(ctx context.Context, board *core.Board) core.BoardView {
	in := c.inputFor(ctx, board)
	view := core.BuildBoardView(board, in)
	view.Diagnostics = append(view.Diagnostics, board.Validate(c.declared, c.configs)...)
	if board.Kind == core.BoardScrum && board.Sprint != "" && in.Sprint == nil {
		view.Diagnostics = append(view.Diagnostics, core.Diagnostic{
			Code: core.CodeSprintID, Severity: core.SeverityError, Path: board.Path, Field: "sprint",
			Message: fmt.Sprintf("board %s is scoped to sprint %s, which this team repository does not hold",
				board.ID, board.Sprint),
		})
	}
	view.Diagnostics = append(view.Diagnostics, c.snapshots.Diagnostics()...)
	return view
}

// MoveCard moves one card: it writes the new status into the item file, in its
// own project repository, and the new position into the board's order list, in
// the team repository. Nothing else is touched (docs/04 R-MOVE-1).
//
// The two writes are ordered so that the board can never contradict the item:
// the item goes first, and a failure to write the board rolls the status back.
func (w *Workspace) MoveCard(ctx context.Context, p BoardMoveParams) (BoardMoveResult, error) {
	ref, err := core.ParseRef(p.Ref)
	if err != nil {
		return BoardMoveResult{}, failf("invalid_request", "%v", err)
	}
	c, err := w.boardContext()
	if err != nil {
		return BoardMoveResult{}, err
	}
	board, err := c.team.Vault.Board(ctx, p.Board)
	if err != nil {
		return BoardMoveResult{}, err
	}
	if p.Rev != "" && p.Rev != "*" && string(board.Rev) != p.Rev {
		return BoardMoveResult{}, &Error{
			Code: "stale_revision", Path: board.Path,
			Current: string(board.Rev),
			Message: fmt.Sprintf("board %s was modified since revision %s (current %s)",
				board.ID, p.Rev, board.Rev),
		}
	}

	in := c.inputFor(ctx, board)
	view := core.BuildBoardView(board, in)
	move := core.BoardMove{Ref: ref, ToColumn: p.ToColumn, Position: p.Position, Status: core.Status(p.Status)}
	plan, err := core.PlanMoveInSprint(board, view, move, c.configs[ref.Project], in.Sprint)
	if err != nil {
		code := "invalid_request"
		if _, cloned := c.owners[ref.Project]; !cloned {
			code = RemoteCardCode
		}
		return BoardMoveResult{}, failf(code, "%v", err)
	}
	if plan.WIPExceeded && !p.Force {
		return BoardMoveResult{}, failf(WipCode,
			"%s is at its WIP limit of %d; confirm the move to exceed it",
			columnName(board, plan.ToColumn), plan.WIPLimit)
	}

	result := BoardMoveResult{
		Move: MovePlan{
			Ref: ref.String(), FromColumn: plan.FromColumn, ToColumn: plan.ToColumn,
			Status: string(plan.Status), StatusChanged: plan.StatusChanged,
			Choices: statusStrings(plan.Choices),
			WIP: WipStatus{
				Column: plan.ToColumn, Used: plan.WIPUsed,
				Limit: plan.WIPLimit, Exceeded: plan.WIPExceeded,
			},
			Sprint: plan.Sprint, SprintAdd: plan.SprintAdd,
		},
		Writes: []RepoWriteSet{},
	}

	// 1. The sprint, in the team repository. Dragging a card out of the backlog
	// column of a scrum board commits it to the sprint; membership is team-repo
	// state, so it is written first and undone if anything below fails
	// (docs/04 sections 5.5 and 11).
	if plan.SprintAdd && in.Sprint != nil {
		in.Sprint.AddItem(ref.String())
		_, sprintWrites, err := c.team.Vault.WriteSprint(ctx, in.Sprint, in.Sprint.Rev)
		if err != nil {
			return BoardMoveResult{}, err
		}
		result.Writes = append(result.Writes, teamWrites(c.team.ID, sprintWrites))
	}
	undoSprint := func() {
		if !plan.SprintAdd || in.Sprint == nil {
			return
		}
		in.Sprint.RemoveItem(ref.String())
		_, _, _ = c.team.Vault.WriteSprint(ctx, in.Sprint, in.Sprint.Rev)
	}

	// 2. The item, in its own repository.
	var previous core.Status
	owner := c.owners[ref.Project]
	if plan.StatusChanged {
		if owner == nil {
			return BoardMoveResult{}, failf(RemoteCardCode,
				"project %s is not cloned on this machine; clone it to move this item", ref.Project)
		}
		for _, column := range view.Columns {
			for _, card := range column.Cards {
				if card.Ref == ref.String() {
					previous = card.Status
				}
			}
		}
		expected := core.Rev(p.ItemRev)
		item, writes, err := owner.Vault.MoveItemStatus(ctx, ref.Item, plan.Status, expected, p.Force)
		if err != nil {
			undoSprint()
			return BoardMoveResult{}, err
		}
		result.Item = item
		result.Writes = append(result.Writes, RepoWriteSet{
			VaultID: owner.ID, Written: writes.Written, Removed: writes.Removed,
		})
	}

	// 3. The board, in the team repository. A failure here rolls the item back
	// so that the board never shows a position that contradicts the item.
	core.ApplyMove(board, move)
	_, boardWrites, err := c.team.Vault.WriteBoard(ctx, board, board.Rev)
	if err != nil {
		if plan.StatusChanged && owner != nil && previous != "" {
			_, _, rollback := owner.Vault.MoveItemStatus(ctx, ref.Item, previous, "", true)
			if rollback != nil {
				undoSprint()
				return BoardMoveResult{}, failf("internal",
					"the board write failed (%v) and %s could not be rolled back to %s (%v)",
					err, ref.Item, previous, rollback)
			}
		}
		undoSprint()
		return BoardMoveResult{}, err
	}
	result.Writes = append(result.Writes, RepoWriteSet{
		VaultID: c.team.ID, Written: boardWrites.Written, Removed: boardWrites.Removed,
	})

	result.Board = c.render(ctx, board)
	return result, nil
}

// columnName returns the display name of a column, falling back to its id.
func columnName(b *core.Board, id string) string {
	if c, ok := b.Column(id); ok && c.Name != "" {
		return c.Name
	}
	return id
}

// statusStrings renders a status list for the wire.
func statusStrings(list []core.Status) []string {
	if len(list) == 0 {
		return nil
	}
	out := make([]string, 0, len(list))
	for _, s := range list {
		out = append(out, string(s))
	}
	return out
}

// BoardPatch is the set of board fields "board.update" may change: the view
// itself — columns, WIP limits, filters, swimlanes, card display — and, on a
// scrum board, the sprint it is scoped to. An absent field is left alone, and
// the card order is never patched here: it moves one card at a time
// (docs/07 section 5.5, story GIT-US-0018).
type BoardPatch struct {
	Title         *string                `json:"title,omitempty"`
	Description   *string                `json:"description,omitempty"`
	Projects      *[]core.ProjectKey     `json:"projects,omitempty"`
	Columns       *[]core.BoardColumn    `json:"columns,omitempty"`
	Filters       *core.BoardFilters     `json:"filters,omitempty"`
	Swimlanes     *core.BoardSwimlanes   `json:"swimlanes,omitempty"`
	Card          *core.BoardCardDisplay `json:"card,omitempty"`
	Sprint        *string                `json:"sprint,omitempty"`
	BacklogColumn *string                `json:"backlogColumn,omitempty"`
}

// BoardUpdateParams is the input of "board.update".
type BoardUpdateParams struct {
	Board string     `json:"board"`
	Rev   string     `json:"rev,omitempty"`
	Patch BoardPatch `json:"patch"`
}

// BoardUpdateResult is the answer of "board.update": the board as it now
// renders, and what the team repository has to save.
type BoardUpdateResult struct {
	Board  core.BoardView `json:"board"`
	Writes []RepoWriteSet `json:"writes"`
}

// UpdateBoard rewrites the board file of the team repository and nothing else.
// It refuses a change that would leave the file invalid — an unknown column in
// `backlog_column`, a sprint on a kanban board, a sprint the repository does
// not hold — so that the UI never has to repair a board it has just broken.
func (w *Workspace) UpdateBoard(ctx context.Context, p BoardUpdateParams) (BoardUpdateResult, error) {
	c, err := w.boardContext()
	if err != nil {
		return BoardUpdateResult{}, err
	}
	board, err := c.team.Vault.Board(ctx, p.Board)
	if err != nil {
		return BoardUpdateResult{}, err
	}
	if p.Rev != "" && p.Rev != "*" && string(board.Rev) != p.Rev {
		return BoardUpdateResult{}, &Error{
			Code: "stale_revision", Path: board.Path,
			Current: string(board.Rev),
			Message: fmt.Sprintf("board %s was modified since revision %s (current %s)",
				board.ID, p.Rev, board.Rev),
		}
	}

	patch := p.Patch
	if patch.Title != nil {
		board.Title = *patch.Title
	}
	if patch.Description != nil {
		board.Description = *patch.Description
	}
	if patch.Projects != nil {
		board.Projects = append([]core.ProjectKey(nil), *patch.Projects...)
	}
	if patch.Columns != nil {
		board.Columns = append([]core.BoardColumn(nil), *patch.Columns...)
	}
	if patch.Filters != nil {
		board.Filters = *patch.Filters
	}
	if patch.Swimlanes != nil {
		board.Swimlanes = *patch.Swimlanes
	}
	if patch.Card != nil {
		board.Card = *patch.Card
	}
	if patch.Sprint != nil {
		board.Sprint = *patch.Sprint
	}
	if patch.BacklogColumn != nil {
		board.BacklogColumn = *patch.BacklogColumn
	}

	if board.Kind == core.BoardScrum && board.Sprint != "" {
		if _, err := c.team.Vault.Sprint(ctx, board.Sprint); err != nil {
			return BoardUpdateResult{}, failf("not_found",
				"no sprint %q in this team repository", board.Sprint)
		}
	}
	for _, d := range board.Validate(c.declared, c.configs) {
		if d.Severity == core.SeverityError {
			return BoardUpdateResult{}, failf("validation_failed", "%s: %s", d.Code, d.Message)
		}
	}
	// A column the board no longer declares takes its order list with it
	// (R-ORD-2); the order of the columns that stayed is untouched.
	core.PruneOrder(board, nil)

	written, writes, err := c.team.Vault.WriteBoard(ctx, board, board.Rev)
	if err != nil {
		return BoardUpdateResult{}, err
	}
	return BoardUpdateResult{
		Board:  c.render(ctx, written),
		Writes: []RepoWriteSet{teamWrites(c.team.ID, writes)},
	}, nil
}
