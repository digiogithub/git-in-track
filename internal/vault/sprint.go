package vault

import (
	"context"
	"fmt"
	"time"

	"github.com/digiogithub/git-in-track/internal/core"
)

// This file is the sprint half of the team surface: "sprint.list",
// "sprint.get", "sprint.create", "sprint.update", "sprint.start" and
// "sprint.close" (docs/04 section 8, story GIT-US-0018).
//
// A sprint file lives in the team repository and its items live in the project
// repositories, so — like a board — the calls are answered by the workspace.
// Every decision (what a sprint scope renders to, whether two sprints overlap,
// what closing one reports) is taken by internal/core; what is here is
// plumbing: read the files, hand the pieces to the core, write the answer back.

// SprintOverlapCode is the machine code of a create or a date change that would
// make two sprints of the same board cover a common day (docs/04 section 8.4).
const SprintOverlapCode = "sprint_overlap"

// SprintActiveCode is the machine code of starting a sprint on a board that is
// already running one.
const SprintActiveCode = "sprint_already_active"

// SprintListParams is the input of "sprint.list": both filters are optional and
// ANDed, and an absent one imposes no constraint.
type SprintListParams struct {
	Board string `json:"board,omitempty"`
	State string `json:"state,omitempty"`
}

// SprintListResult is the answer of "sprint.list". The summaries carry the
// metrics of every sprint, so that the picker can show "12 of 21 points" with
// no second call.
type SprintListResult struct {
	Sprints     []core.SprintSummary `json:"sprints"`
	Diagnostics []core.Diagnostic    `json:"diagnostics"`
}

// SprintParams names one sprint.
type SprintParams struct {
	ID string `json:"id"`
}

// SprintCreateParams is the input of "sprint.create". Dates are required; the
// id is allocated from the team key and the sprints already on disk.
type SprintCreateParams struct {
	Board          string   `json:"board"`
	Title          string   `json:"title,omitempty"`
	Start          string   `json:"start"`
	End            string   `json:"end"`
	Goal           string   `json:"goal,omitempty"`
	State          string   `json:"state,omitempty"`
	Items          []string `json:"items,omitempty"`
	CapacityHours  *float64 `json:"capacityHours,omitempty"`
	VelocityTarget *float64 `json:"velocityTarget,omitempty"`
	Participants   []string `json:"participants,omitempty"`
	Author         string   `json:"author,omitempty"`
}

// SprintPatch is the set of sprint fields "sprint.update" may change. An absent
// field is left alone; `addItems` and `removeItems` edit the scope without
// resending it, which is what keeps a planning drag a one-line diff.
type SprintPatch struct {
	Title          *string   `json:"title,omitempty"`
	Goal           *string   `json:"goal,omitempty"`
	Start          *string   `json:"start,omitempty"`
	End            *string   `json:"end,omitempty"`
	State          *string   `json:"state,omitempty"`
	CapacityHours  *float64  `json:"capacityHours,omitempty"`
	VelocityTarget *float64  `json:"velocityTarget,omitempty"`
	Participants   *[]string `json:"participants,omitempty"`
	Items          *[]string `json:"items,omitempty"`
	AddItems       []string  `json:"addItems,omitempty"`
	RemoveItems    []string  `json:"removeItems,omitempty"`
}

// SprintUpdateParams is the input of "sprint.update".
type SprintUpdateParams struct {
	ID    string      `json:"id"`
	Rev   string      `json:"rev,omitempty"`
	Patch SprintPatch `json:"patch"`
}

// SprintStartParams is the input of "sprint.start": the sprint becomes active,
// its scope is copied into `committed`, and the board it belongs to is pointed
// at it (docs/04 section 5.5).
type SprintStartParams struct {
	ID  string `json:"id"`
	Rev string `json:"rev,omitempty"`
	// Force starts a sprint on a board that already runs one. It is refused
	// once, so that two active sprints are never an accident (W-SPRINT-TWO-ACTIVE).
	Force bool `json:"force,omitempty"`
}

// SprintCarry is one closing decision: what happens to an unfinished item
// (R-SPR-3). Nothing is implicit — an item nobody decided about is left alone.
type SprintCarry struct {
	Ref    string `json:"ref"`
	Action string `json:"action"`
	// Sprint is the sprint an item is carried into, for `next`. Empty picks the
	// earliest planned sprint of the same board.
	Sprint string `json:"sprint,omitempty"`
	// Status overrides the status a `backlog` decision writes.
	Status string `json:"status,omitempty"`
}

// SprintCloseParams is the input of "sprint.close".
type SprintCloseParams struct {
	ID    string        `json:"id"`
	Rev   string        `json:"rev,omitempty"`
	Carry []SprintCarry `json:"carry,omitempty"`
}

// SprintResult is the answer of every sprint call that writes: the sprint as it
// now renders, the board when the write touched it, the closing report when
// there is one, and what each repository has to save.
type SprintResult struct {
	Sprint core.SprintView         `json:"sprint"`
	Board  *core.BoardView         `json:"board,omitempty"`
	Report *core.SprintCloseReport `json:"report,omitempty"`
	Writes []RepoWriteSet          `json:"writes"`
}

// ------------------------------------------------------------------ vault ---

// sprintStore returns the store over this vault's `.pmngr/sprints/`. The caller
// holds the lock.
func (v *Vault) sprintStore() (*core.SprintStore, error) {
	if v.team == nil {
		return nil, failf("not_found", "this repository has no %s at its root", core.TeamFileName)
	}
	store := core.NewSprintStore(v.fs, v.team.TeamDirPath)
	if v.now != nil {
		store.Clock = core.ClockFunc(v.now)
	}
	return store, nil
}

// Now is the clock the vault stamps its writes with.
func (v *Vault) Now() time.Time {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.now == nil {
		return time.Now()
	}
	return v.now()
}

// Sprints returns every sprint of a team repository, with the diagnostics of
// the files that could not be parsed. It takes the vault lock.
func (v *Vault) Sprints(ctx context.Context) ([]*core.Sprint, []core.Diagnostic, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	store, err := v.sprintStore()
	if err != nil {
		return nil, nil, err
	}
	sprints, diags, err := store.List(ctx)
	if err != nil {
		return nil, nil, failf("internal", "%v", err)
	}
	return sprints, diags, nil
}

// Sprint reads one sprint by its id. It takes the vault lock.
func (v *Vault) Sprint(ctx context.Context, id string) (*core.Sprint, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	store, err := v.sprintStore()
	if err != nil {
		return nil, err
	}
	sprint, err := store.Get(ctx, id)
	if err != nil {
		return nil, failf("not_found", "no sprint %q in %s", id, store.Dir())
	}
	return sprint, nil
}

// NextSprintID allocates the next sprint id of the team repository.
func (v *Vault) NextSprintID(ctx context.Context) (string, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	store, err := v.sprintStore()
	if err != nil {
		return "", err
	}
	if v.team == nil || v.team.Config == nil {
		return "", failf("not_found", "this repository has no %s at its root", core.TeamFileName)
	}
	id, err := store.NextID(ctx, v.team.Config.Key)
	if err != nil {
		return "", failf("internal", "%v", err)
	}
	return id, nil
}

// WriteSprint persists a sprint and reports what the host must save. It takes
// the vault lock.
func (v *Vault) WriteSprint(
	ctx context.Context, s *core.Sprint, expected core.Rev,
) (*core.Sprint, WriteSet, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	store, err := v.sprintStore()
	if err != nil {
		return nil, WriteSet{}, err
	}
	v.fs.begin()
	written, err := store.Write(ctx, s, expected)
	if err != nil {
		return nil, WriteSet{}, fmt.Errorf("write sprint: %w", err)
	}
	writes, err := v.commit(ctx)
	if err != nil {
		return nil, WriteSet{}, err
	}
	return written, writes, nil
}

// -------------------------------------------------------------- workspace ---

// sprintContext is a board context plus every sprint of the team repository:
// what the cross-sprint rules (one active sprint, no overlapping dates) need.
type sprintContext struct {
	boardContext
	sprints []*core.Sprint
	diags   []core.Diagnostic
}

// sprintContext gathers the sprints of the team repository.
func (w *Workspace) sprintContext(ctx context.Context) (sprintContext, error) {
	c, err := w.boardContext()
	if err != nil {
		return sprintContext{}, err
	}
	sprints, diags, err := c.team.Vault.Sprints(ctx)
	if err != nil {
		return sprintContext{}, err
	}
	return sprintContext{boardContext: c, sprints: sprints, diags: diags}, nil
}

// board reads the board a sprint belongs to, nil when the sprint names one the
// repository does not hold. A sprint whose board is missing still renders; the
// validation reports it (E-SPRINT-BOARD).
func (c sprintContext) board(ctx context.Context, id string) *core.Board {
	if id == "" {
		return nil
	}
	board, err := c.team.Vault.Board(ctx, id)
	if err != nil {
		return nil
	}
	return board
}

// boardIDs is every board id of the team repository, for the validation.
func (c sprintContext) boardIDs(ctx context.Context) []string {
	boards, _, err := c.team.Vault.Boards(ctx)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(boards))
	for _, b := range boards {
		out = append(out, b.ID)
	}
	return out
}

// validateInput is the context the file-level sprint validation needs.
func (c sprintContext) validateInput(ctx context.Context) core.SprintValidateInput {
	in := core.SprintValidateInput{
		Boards: c.boardIDs(ctx), Declared: c.declared,
		Cloned: map[core.ProjectKey]bool{},
	}
	if team := c.team.Vault.Team(); team != nil && team.Config != nil {
		in.TeamKey = team.Config.Key
	}
	for key := range c.owners {
		in.Cloned[key] = true
	}
	return in
}

// view renders one sprint over every open repository.
func (c sprintContext) view(ctx context.Context, s *core.Sprint) core.SprintView {
	board := c.board(ctx, s.Board)
	view := core.BuildSprintView(s, board, c.input(ctx))
	view.Diagnostics = append(view.Diagnostics, s.Validate(c.validateInput(ctx))...)
	view.Diagnostics = append(view.Diagnostics, core.ValidateSprintSet(c.sprints)...)
	return view
}

// Sprints lists the sprints of the team repository, filtered by board and by
// state, newest first — the order a sprint picker reads in.
func (w *Workspace) Sprints(ctx context.Context, p SprintListParams) (SprintListResult, error) {
	c, err := w.sprintContext(ctx)
	if err != nil {
		return SprintListResult{}, err
	}
	if p.State != "" && !core.SprintState(p.State).Valid() {
		return SprintListResult{}, failf("invalid_request",
			"%q is not a sprint state: planned, active or closed", p.State)
	}
	out := SprintListResult{Sprints: []core.SprintSummary{}, Diagnostics: []core.Diagnostic{}}
	out.Diagnostics = append(out.Diagnostics, c.diags...)
	out.Diagnostics = append(out.Diagnostics, core.ValidateSprintSet(c.sprints)...)
	in := c.input(ctx)
	for _, s := range c.sprints {
		if p.Board != "" && s.Board != p.Board {
			continue
		}
		if p.State != "" && string(s.State) != p.State {
			continue
		}
		board := c.board(ctx, s.Board)
		out.Sprints = append(out.Sprints, core.BuildSprintView(s, board, in).Sprint)
	}
	return out, nil
}

// Sprint renders one sprint: its scope, the candidates the board would offer
// and the findings of its validation.
func (w *Workspace) Sprint(ctx context.Context, id string) (core.SprintView, error) {
	c, err := w.sprintContext(ctx)
	if err != nil {
		return core.SprintView{}, err
	}
	s, err := c.team.Vault.Sprint(ctx, id)
	if err != nil {
		return core.SprintView{}, err
	}
	return c.view(ctx, s), nil
}

// CreateSprint writes a new sprint file into the team repository. It refuses a
// date range that overlaps another sprint of the same board, because two
// sprints covering the same day make "the active sprint" meaningless
// (docs/04 section 8.4).
func (w *Workspace) CreateSprint(ctx context.Context, p SprintCreateParams) (SprintResult, error) {
	c, err := w.sprintContext(ctx)
	if err != nil {
		return SprintResult{}, err
	}
	if p.Board == "" {
		return SprintResult{}, failf("invalid_request", "a sprint needs the board it belongs to")
	}
	if c.board(ctx, p.Board) == nil {
		return SprintResult{}, failf("not_found", "no board %q in this team repository", p.Board)
	}
	start, err := parseSprintDate("start", p.Start)
	if err != nil {
		return SprintResult{}, err
	}
	end, err := parseSprintDate("end", p.End)
	if err != nil {
		return SprintResult{}, err
	}
	state := core.SprintPlanned
	if p.State != "" {
		state = core.SprintState(p.State)
		if !state.Valid() {
			return SprintResult{}, failf("invalid_request",
				"%q is not a sprint state: planned, active or closed", p.State)
		}
	}
	id, err := c.team.Vault.NextSprintID(ctx)
	if err != nil {
		return SprintResult{}, err
	}
	sprint := &core.Sprint{
		ID: id, Type: "sprint", Title: p.Title, Board: p.Board, State: state,
		Start: start, End: end, Goal: p.Goal,
		CapacityHours: p.CapacityHours, VelocityTarget: p.VelocityTarget,
		Participants: p.Participants, Author: p.Author,
		Created: core.NewTimestamp(c.team.Vault.Now()),
		Items:   []string{},
	}
	for _, raw := range p.Items {
		ref, err := core.ParseRef(raw)
		if err != nil {
			return SprintResult{}, failf("invalid_request", "%v", err)
		}
		sprint.AddItem(ref.String())
	}
	if err := checkSprintDates(sprint, c.sprints); err != nil {
		return SprintResult{}, err
	}
	if state == core.SprintActive {
		if err := checkNoActiveSprint(sprint, c.sprints, false); err != nil {
			return SprintResult{}, err
		}
		sprint.Committed = append([]string(nil), sprint.Items...)
	}
	written, writes, err := c.team.Vault.WriteSprint(ctx, sprint, "")
	if err != nil {
		return SprintResult{}, err
	}
	return c.result(ctx, written, []RepoWriteSet{teamWrites(c.team.ID, writes)}), nil
}

// UpdateSprint changes the fields of one sprint file and nothing else: the
// goal, the dates, the participants and the scope all live in the team
// repository, so moving an item in or out of a sprint never writes into a
// project repository (docs/04 section 11, R-SPR-2).
func (w *Workspace) UpdateSprint(ctx context.Context, p SprintUpdateParams) (SprintResult, error) {
	c, err := w.sprintContext(ctx)
	if err != nil {
		return SprintResult{}, err
	}
	sprint, err := c.team.Vault.Sprint(ctx, p.ID)
	if err != nil {
		return SprintResult{}, err
	}
	if err := checkSprintRev(sprint, p.Rev); err != nil {
		return SprintResult{}, err
	}
	patch := p.Patch
	if patch.Title != nil {
		sprint.Title = *patch.Title
	}
	if patch.Goal != nil {
		sprint.Goal = *patch.Goal
	}
	if patch.Start != nil {
		start, err := parseSprintDate("start", *patch.Start)
		if err != nil {
			return SprintResult{}, err
		}
		sprint.Start = start
	}
	if patch.End != nil {
		end, err := parseSprintDate("end", *patch.End)
		if err != nil {
			return SprintResult{}, err
		}
		sprint.End = end
	}
	if patch.State != nil {
		state := core.SprintState(*patch.State)
		if !state.Valid() {
			return SprintResult{}, failf("invalid_request",
				"%q is not a sprint state: planned, active or closed", *patch.State)
		}
		if state == core.SprintActive && sprint.State != core.SprintActive {
			if err := checkNoActiveSprint(sprint, c.sprints, false); err != nil {
				return SprintResult{}, err
			}
		}
		sprint.State = state
	}
	if patch.CapacityHours != nil {
		sprint.CapacityHours = patch.CapacityHours
	}
	if patch.VelocityTarget != nil {
		sprint.VelocityTarget = patch.VelocityTarget
	}
	if patch.Participants != nil {
		sprint.Participants = append([]string(nil), *patch.Participants...)
	}
	if patch.Items != nil {
		refs, err := parseRefs(*patch.Items)
		if err != nil {
			return SprintResult{}, err
		}
		sprint.Items = refs
	}
	added, err := parseRefs(patch.AddItems)
	if err != nil {
		return SprintResult{}, err
	}
	for _, ref := range added {
		sprint.AddItem(ref)
	}
	removed, err := parseRefs(patch.RemoveItems)
	if err != nil {
		return SprintResult{}, err
	}
	for _, ref := range removed {
		sprint.RemoveItem(ref)
	}
	if sprint.Items == nil {
		sprint.Items = []string{}
	}
	if err := checkSprintDates(sprint, c.sprints); err != nil {
		return SprintResult{}, err
	}
	written, writes, err := c.team.Vault.WriteSprint(ctx, sprint, sprint.Rev)
	if err != nil {
		return SprintResult{}, err
	}
	return c.result(ctx, written, []RepoWriteSet{teamWrites(c.team.ID, writes)}), nil
}

// StartSprint makes a sprint active: it copies the scope into `committed`, so
// that what was promised stays legible next to what was added later (R-SPR-1),
// and points its board at it. Both writes stay in the team repository.
func (w *Workspace) StartSprint(ctx context.Context, p SprintStartParams) (SprintResult, error) {
	c, err := w.sprintContext(ctx)
	if err != nil {
		return SprintResult{}, err
	}
	sprint, err := c.team.Vault.Sprint(ctx, p.ID)
	if err != nil {
		return SprintResult{}, err
	}
	if err := checkSprintRev(sprint, p.Rev); err != nil {
		return SprintResult{}, err
	}
	if sprint.State == core.SprintClosed {
		return SprintResult{}, failf("invalid_request",
			"sprint %s is closed; a closed sprint is history and is never restarted", sprint.ID)
	}
	if err := checkNoActiveSprint(sprint, c.sprints, p.Force); err != nil {
		return SprintResult{}, err
	}
	sprint.State = core.SprintActive
	sprint.Committed = append([]string(nil), sprint.Items...)
	written, writes, err := c.team.Vault.WriteSprint(ctx, sprint, sprint.Rev)
	if err != nil {
		return SprintResult{}, err
	}
	sets := []RepoWriteSet{teamWrites(c.team.ID, writes)}

	// The board follows the sprint it runs, so that opening the board shows the
	// sprint that has just started (docs/04 section 5.5).
	var boardView *core.BoardView
	if board := c.board(ctx, sprint.Board); board != nil && board.Sprint != sprint.ID {
		board.Sprint = sprint.ID
		if _, boardWrites, err := c.team.Vault.WriteBoard(ctx, board, board.Rev); err == nil {
			sets = append(sets, teamWrites(c.team.ID, boardWrites))
			view := c.render(ctx, board)
			boardView = &view
		} else {
			return SprintResult{}, err
		}
	}
	result := c.result(ctx, written, sets)
	result.Board = boardView
	return result, nil
}

// CloseSprint closes a sprint and reports what was finished and what was not.
// Closing modifies no item by itself: the caller decides per unfinished item
// whether to leave it, carry it into another sprint or send it back to the
// backlog, and only those decisions write anything (R-SPR-3).
func (w *Workspace) CloseSprint(ctx context.Context, p SprintCloseParams) (SprintResult, error) {
	c, err := w.sprintContext(ctx)
	if err != nil {
		return SprintResult{}, err
	}
	sprint, err := c.team.Vault.Sprint(ctx, p.ID)
	if err != nil {
		return SprintResult{}, err
	}
	if err := checkSprintRev(sprint, p.Rev); err != nil {
		return SprintResult{}, err
	}
	view := c.view(ctx, sprint)
	report := core.SummarizeClose(sprint, view)

	var sets []RepoWriteSet
	for _, decision := range p.Carry {
		outcome, written := c.carry(ctx, sprint, decision)
		report.Carried = append(report.Carried, outcome)
		sets = append(sets, written...)
	}

	sprint.State = core.SprintClosed
	written, writes, err := c.team.Vault.WriteSprint(ctx, sprint, sprint.Rev)
	if err != nil {
		return SprintResult{}, err
	}
	sets = append(sets, teamWrites(c.team.ID, writes))
	result := c.result(ctx, written, sets)
	result.Report = &report
	return result, nil
}

// carry applies one closing decision and reports what it did. A decision that
// cannot be applied is reported on its own entry; the rest of the closing goes
// through, because a sprint that half-closed would be worse than one that
// closed with a list of what could not be moved.
func (c sprintContext) carry(
	ctx context.Context, sprint *core.Sprint, decision SprintCarry,
) (core.SprintCarryResult, []RepoWriteSet) {
	out := core.SprintCarryResult{Ref: decision.Ref, Action: core.SprintCarryAction(decision.Action)}
	if !out.Action.Valid() {
		out.Error = fmt.Sprintf("%q is not a closing choice: leave, next or backlog", decision.Action)
		return out, nil
	}
	ref, err := core.ParseRef(decision.Ref)
	if err != nil {
		out.Error = err.Error()
		return out, nil
	}
	if !sprint.Has(ref.String()) {
		out.Error = fmt.Sprintf("%s is not in sprint %s", ref, sprint.ID)
		return out, nil
	}
	switch out.Action {
	case core.CarryLeave:
		return out, nil
	case core.CarryNext:
		target := c.nextSprint(sprint, decision.Sprint)
		if target == nil {
			out.Error = fmt.Sprintf(
				"no sprint to carry %s into: name one, or plan the next sprint of board %s first",
				ref, sprint.Board)
			return out, nil
		}
		if !target.AddItem(ref.String()) {
			out.Sprint = target.ID
			return out, nil
		}
		_, writes, err := c.team.Vault.WriteSprint(ctx, target, target.Rev)
		if err != nil {
			out.Error = err.Error()
			return out, nil
		}
		out.Sprint = target.ID
		return out, []RepoWriteSet{teamWrites(c.team.ID, writes)}
	case core.CarryBacklog:
		owner, cloned := c.owners[ref.Project]
		if !cloned {
			out.Error = fmt.Sprintf(
				"project %s is not cloned on this machine; clone it to send %s back to the backlog",
				ref.Project, ref.Item)
			return out, nil
		}
		status := core.Status(decision.Status)
		if status == "" {
			status = core.BacklogStatus(c.configs[ref.Project])
		}
		if status == "" {
			out.Error = fmt.Sprintf("the workflow of %s declares no backlog status", ref.Project)
			return out, nil
		}
		// The closing dialog is the confirmation R-MOVE-2 asks for: sending an
		// unfinished item back to the backlog is written even when the project
		// workflow does not declare that transition.
		_, writes, err := owner.Vault.MoveItemStatus(ctx, ref.Item, status, "", true)
		if err != nil {
			out.Error = err.Error()
			return out, nil
		}
		out.Status = status
		return out, []RepoWriteSet{{VaultID: owner.ID, Written: writes.Written, Removed: writes.Removed}}
	}
	return out, nil
}

// nextSprint picks the sprint an item is carried into: the one the caller
// named, or the earliest planned sprint of the same board.
func (c sprintContext) nextSprint(from *core.Sprint, named string) *core.Sprint {
	var best *core.Sprint
	for _, candidate := range c.sprints {
		if candidate.ID == from.ID {
			continue
		}
		if named != "" {
			if candidate.ID == named {
				return candidate
			}
			continue
		}
		if candidate.Board != from.Board || candidate.State != core.SprintPlanned {
			continue
		}
		if best == nil || candidate.Start.Before(best.Start.Time) {
			best = candidate
		}
	}
	return best
}

// result renders a sprint after a write, so that the caller never has to read
// it back to see what it now looks like.
func (c sprintContext) result(ctx context.Context, s *core.Sprint, writes []RepoWriteSet) SprintResult {
	if writes == nil {
		writes = []RepoWriteSet{}
	}
	return SprintResult{Sprint: c.view(ctx, s), Writes: writes}
}

// teamWrites tags a write set with the repository it belongs to.
func teamWrites(vaultID string, writes WriteSet) RepoWriteSet {
	return RepoWriteSet{VaultID: vaultID, Written: writes.Written, Removed: writes.Removed}
}

// parseSprintDate decodes a required `YYYY-MM-DD` field.
func parseSprintDate(field, value string) (core.Date, error) {
	if value == "" {
		return core.Date{}, failf("invalid_request", "a sprint needs a %s date", field)
	}
	date, err := core.ParseDate(value)
	if err != nil {
		return core.Date{}, failf("invalid_request", "%v", err)
	}
	return date, nil
}

// parseRefs validates a list of `<projectKey>/<itemId>` references.
func parseRefs(raw []string) ([]string, error) {
	out := make([]string, 0, len(raw))
	for _, entry := range raw {
		ref, err := core.ParseRef(entry)
		if err != nil {
			return nil, failf("invalid_request", "%v", err)
		}
		out = append(out, ref.String())
	}
	return out, nil
}

// checkSprintRev enforces the optimistic lock of a sprint write.
func checkSprintRev(s *core.Sprint, rev string) error {
	if rev == "" || rev == "*" || string(s.Rev) == rev {
		return nil
	}
	return &Error{
		Code: "stale_revision", Path: s.Path,
		Message: fmt.Sprintf("sprint %s was modified since revision %s (current %s)", s.ID, rev, s.Rev),
	}
}

// checkSprintDates refuses a date range that overlaps another sprint of the
// same board. The file-level validation reports the same condition as a warning
// for files that are already on disk; a write is where it can still be stopped.
func checkSprintDates(s *core.Sprint, others []*core.Sprint) error {
	if s.Start.IsZero() || s.End.IsZero() {
		return failf("invalid_request", "a sprint needs a start and an end date")
	}
	if s.End.Before(s.Start.Time) {
		return failf("invalid_request", "the end date %s is before the start date %s", s.End, s.Start)
	}
	if other := core.OverlappingSprint(s, others); other != nil {
		return failf(SprintOverlapCode,
			"%s to %s overlaps sprint %s (%s to %s) on board %s; sprints on one board cannot share a day",
			s.Start, s.End, other.ID, other.Start, other.End, s.Board)
	}
	return nil
}

// checkNoActiveSprint refuses a second active sprint on one board unless the
// caller confirms it (W-SPRINT-TWO-ACTIVE).
func checkNoActiveSprint(s *core.Sprint, others []*core.Sprint, force bool) error {
	if force {
		return nil
	}
	for _, other := range others {
		if other.ID == s.ID || other.Board != s.Board || other.State != core.SprintActive {
			continue
		}
		return failf(SprintActiveCode,
			"board %s is already running sprint %s; close it first, or confirm to run two at once",
			s.Board, other.ID)
	}
	return nil
}
