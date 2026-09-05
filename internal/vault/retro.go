package vault

import (
	"context"
	"fmt"

	"github.com/digiogithub/git-in-track/internal/core"
)

// This file is the retrospective half of the team surface: "retro.list",
// "retro.get", "retro.create", "retro.update" and "retro.promote"
// (docs/04 section 9, story GIT-US-0027).
//
// A retro file lives in the team repository; the tasks its improvement actions
// were promoted into live in the project repositories. Promotion is therefore
// the one retro call that writes to two repositories at once, and the only one
// that can be refused because a project is not cloned (R-RETRO-2).
//
// Every decision — what a retro renders to, which of its actions are still
// open, what task an action becomes — is taken by internal/core; what is here
// is plumbing.

// RetroNotClonedCode is the machine code of promoting an action into a project
// no open repository serves (R-RETRO-2).
const RetroNotClonedCode = "repo_not_cloned"

// RetroActionPromotedCode is the machine code of promoting an action twice.
const RetroActionPromotedCode = "retro_action_promoted"

// RetroListParams is the input of "retro.list": both filters are optional and
// ANDed, and an absent one imposes no constraint.
type RetroListParams struct {
	Sprint string `json:"sprint,omitempty"`
	Board  string `json:"board,omitempty"`
	State  string `json:"state,omitempty"`
}

// RetroListResult is the answer of "retro.list", newest retro first. Each
// summary carries the follow-through counts, so an index can show "2 of 3
// actions still open" with no second call.
type RetroListResult struct {
	Retros []core.RetroSummary `json:"retros"`
	// Carried are the improvement actions of these retros that are still open,
	// with the live state of the tasks they were promoted into. It is what a
	// team looks at before writing a new retro.
	Carried     []core.RetroActionView `json:"carried"`
	Diagnostics []core.Diagnostic      `json:"diagnostics"`
}

// RetroParams names one retro.
type RetroParams struct {
	ID string `json:"id"`
}

// RetroCreateParams is the input of "retro.create". Everything except the
// sprint has a sensible default, because the friction of starting a retro is
// exactly what stops teams running them.
type RetroCreateParams struct {
	// Sprint is the sprint being reviewed. It is optional: an incident retro
	// belongs to no sprint (docs/04 section 9.2).
	Sprint string `json:"sprint,omitempty"`
	Board  string `json:"board,omitempty"`
	Title  string `json:"title,omitempty"`
	// Date defaults to today in the workspace clock.
	Date         string   `json:"date,omitempty"`
	Facilitator  string   `json:"facilitator,omitempty"`
	Participants []string `json:"participants,omitempty"`
	Anonymous    bool     `json:"anonymous,omitempty"`
	// VotesPerPerson defaults to three (docs/04 section 9.2).
	VotesPerPerson *int `json:"votesPerPerson,omitempty"`
	// CarriedFrom defaults to the most recent retro of the same board, which
	// is what makes the open actions of last time show up in this one.
	CarriedFrom string `json:"carriedFrom,omitempty"`
	State       string `json:"state,omitempty"`
	Author      string `json:"author,omitempty"`
}

// RetroNoteDraft is one sticky note added during the session.
type RetroNoteDraft struct {
	Category string `json:"category"`
	Text     string `json:"text"`
	Author   string `json:"author,omitempty"`
}

// RetroNoteEdit changes one note. An absent field is left alone; `category`
// moves the note to another column, which is how the room reclassifies a
// sticky mid-session.
type RetroNoteEdit struct {
	ID       string  `json:"id"`
	Text     *string `json:"text,omitempty"`
	Author   *string `json:"author,omitempty"`
	Category string  `json:"category,omitempty"`
}

// RetroActionDraft is one improvement action selected during the session.
type RetroActionDraft struct {
	ID    string `json:"id,omitempty"`
	Title string `json:"title"`
	Owner string `json:"owner,omitempty"`
	Due   string `json:"due,omitempty"`
	Theme string `json:"theme,omitempty"`
	Note  string `json:"note,omitempty"`
}

// RetroActionEdit changes one action. `task` is not editable here: a task
// reference is written by "retro.promote" and by nothing else.
type RetroActionEdit struct {
	ID     string  `json:"id"`
	Title  *string `json:"title,omitempty"`
	Owner  *string `json:"owner,omitempty"`
	Due    *string `json:"due,omitempty"`
	Theme  *string `json:"theme,omitempty"`
	Status *string `json:"status,omitempty"`
	Note   *string `json:"note,omitempty"`
}

// RetroPatch is the set of retro fields "retro.update" may change. An absent
// field is left alone; the note and action lists are edited entry by entry so
// that one participant's write is one line of diff (docs/04 section 9.1).
type RetroPatch struct {
	Title          *string   `json:"title,omitempty"`
	Date           *string   `json:"date,omitempty"`
	State          *string   `json:"state,omitempty"`
	Facilitator    *string   `json:"facilitator,omitempty"`
	Participants   *[]string `json:"participants,omitempty"`
	Anonymous      *bool     `json:"anonymous,omitempty"`
	VotesPerPerson *int      `json:"votesPerPerson,omitempty"`
	CarriedFrom    *string   `json:"carriedFrom,omitempty"`

	AddNotes    []RetroNoteDraft `json:"addNotes,omitempty"`
	UpdateNotes []RetroNoteEdit  `json:"updateNotes,omitempty"`
	RemoveNotes []string         `json:"removeNotes,omitempty"`

	// Themes replaces the grouping wholesale: merging duplicates is one
	// decision about the whole wall, not an edit to one sticky.
	Themes *[]core.RetroTheme `json:"themes,omitempty"`
	// Votes replaces the ballot wholesale, for the same reason.
	Votes *map[string][]string `json:"votes,omitempty"`

	AddActions    []RetroActionDraft `json:"addActions,omitempty"`
	UpdateActions []RetroActionEdit  `json:"updateActions,omitempty"`
	RemoveActions []string           `json:"removeActions,omitempty"`
}

// RetroUpdateParams is the input of "retro.update".
type RetroUpdateParams struct {
	ID    string     `json:"id"`
	Rev   string     `json:"rev,omitempty"`
	Patch RetroPatch `json:"patch"`
}

// RetroPromoteParams is the input of "retro.promote": turn one improvement
// action into a real task in a project repository (R-RETRO-2).
type RetroPromoteParams struct {
	ID     string `json:"id"`
	Action string `json:"action"`
	// Project is the repository the task is created in.
	Project string `json:"project"`
	// Labels overrides the `[retro]` label the task carries (R-RETRO-3).
	Labels []string `json:"labels,omitempty"`
	Rev    string   `json:"rev,omitempty"`
}

// RetroResult is the answer of every retro call that writes: the retro as it
// now renders, the task a promotion created, and what each repository has to
// save.
type RetroResult struct {
	Retro core.RetroView `json:"retro"`
	// Task is the item a promotion produced, in the project repository.
	Task   *core.Item     `json:"task,omitempty"`
	Writes []RepoWriteSet `json:"writes"`
}

// ------------------------------------------------------------------ vault ---

// retroStore returns the store over this vault's `.pmngr/retros/`. The caller
// holds the lock.
func (v *Vault) retroStore() (*core.RetroStore, error) {
	if v.team == nil {
		return nil, failf("not_found", "this repository has no %s at its root", core.TeamFileName)
	}
	store := core.NewRetroStore(v.fs, v.team.TeamDirPath)
	if v.now != nil {
		store.Clock = core.ClockFunc(v.now)
	}
	return store, nil
}

// Retros returns every retro of a team repository, newest first, with the
// diagnostics of the files that could not be parsed. It takes the vault lock.
func (v *Vault) Retros(ctx context.Context) ([]*core.Retro, []core.Diagnostic, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	store, err := v.retroStore()
	if err != nil {
		return nil, nil, err
	}
	retros, diags, err := store.List(ctx)
	if err != nil {
		return nil, nil, failf("internal", "%v", err)
	}
	return retros, diags, nil
}

// Retro reads one retro by its id. It takes the vault lock.
func (v *Vault) Retro(ctx context.Context, id string) (*core.Retro, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	store, err := v.retroStore()
	if err != nil {
		return nil, err
	}
	retro, err := store.Get(ctx, id)
	if err != nil {
		return nil, failf("not_found", "no retro %q in %s", id, store.Dir())
	}
	return retro, nil
}

// NextRetroID allocates the next retro id of the team repository.
func (v *Vault) NextRetroID(ctx context.Context) (string, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	store, err := v.retroStore()
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

// WriteRetro persists a retro and reports what the host must save. It takes the
// vault lock.
func (v *Vault) WriteRetro(
	ctx context.Context, r *core.Retro, expected core.Rev,
) (*core.Retro, WriteSet, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	store, err := v.retroStore()
	if err != nil {
		return nil, WriteSet{}, err
	}
	v.fs.begin()
	written, err := store.Write(ctx, r, expected)
	if err != nil {
		return nil, WriteSet{}, fmt.Errorf("write retro: %w", err)
	}
	writes, err := v.commit(ctx)
	if err != nil {
		return nil, WriteSet{}, err
	}
	return written, writes, nil
}

// CreateItem writes a new item into one project of this repository and reports
// what the host must save. It is what promoting a retro action into a task goes
// through, and it takes the vault lock.
func (v *Vault) CreateItem(
	ctx context.Context, project core.ProjectKey, draft core.ItemDraft,
) (*core.Item, WriteSet, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	store, err := v.storeFor(project)
	if err != nil {
		return nil, WriteSet{}, err
	}
	v.fs.begin()
	item, err := store.Create(ctx, draft)
	if err != nil {
		return nil, WriteSet{}, fmt.Errorf("create item in %s: %w", project, err)
	}
	writes, err := v.commit(ctx)
	if err != nil {
		return nil, WriteSet{}, err
	}
	return item, writes, nil
}

// -------------------------------------------------------------- workspace ---

// retroContext is a sprint context plus every retro of the team repository:
// what the carried-actions rule needs.
type retroContext struct {
	sprintContext
	retros     []*core.Retro
	retroDiags []core.Diagnostic
}

// retroContext gathers the retros of the team repository.
func (w *Workspace) retroContext(ctx context.Context) (retroContext, error) {
	c, err := w.sprintContext(ctx)
	if err != nil {
		return retroContext{}, err
	}
	retros, diags, err := c.team.Vault.Retros(ctx)
	if err != nil {
		return retroContext{}, err
	}
	return retroContext{sprintContext: c, retros: retros, retroDiags: diags}, nil
}

// validateRetroInput is the context the file-level retro validation needs.
func (c retroContext) validateRetroInput() core.RetroValidateInput {
	in := core.RetroValidateInput{Declared: c.declared, Cloned: map[core.ProjectKey]bool{}}
	if team := c.team.Vault.Team(); team != nil && team.Config != nil {
		in.TeamKey = team.Config.Key
	}
	for _, s := range c.sprints {
		in.Sprints = append(in.Sprints, s.ID)
	}
	for key := range c.owners {
		in.Cloned[key] = true
	}
	return in
}

// sprintOf returns the sprint a retro reviews, nil when the team repository
// does not hold it. A retro whose sprint is missing still renders.
func (c retroContext) sprintOf(id string) *core.Sprint {
	for _, s := range c.sprints {
		if s.ID == id {
			return s
		}
	}
	return nil
}

// retroInput is everything BuildRetroView needs: the render context of the open
// repositories, the earlier retros and the sprint under review.
func (c retroContext) retroInput(ctx context.Context, r *core.Retro) core.RetroInput {
	in := core.RetroInput{Board: c.input(ctx), Previous: c.earlier(r)}
	if sprint := c.sprintOf(r.Sprint); sprint != nil {
		in.Sprint = sprint
		view := core.BuildSprintView(sprint, c.board(ctx, sprint.Board), in.Board)
		in.SprintCards = view.Cards
	}
	return in
}

// earlier returns the retros held before this one, newest first. Ordering by
// date is what makes "the previous retro" mean the same thing whatever order
// the files were created in.
func (c retroContext) earlier(r *core.Retro) []*core.Retro {
	out := make([]*core.Retro, 0, len(c.retros))
	for _, candidate := range c.retros {
		if candidate.ID == r.ID {
			continue
		}
		if !r.Date.IsZero() && !candidate.Date.IsZero() && candidate.Date.After(r.Date.Time) {
			continue
		}
		out = append(out, candidate)
	}
	return out
}

// retroView renders one retro over every open repository.
func (c retroContext) retroView(ctx context.Context, r *core.Retro) core.RetroView {
	view := core.BuildRetroView(r, c.retroInput(ctx, r))
	view.Diagnostics = append(view.Diagnostics, r.Validate(c.validateRetroInput())...)
	return view
}

// retroResult renders a retro after a write, so that the caller never has to
// read it back to see what it now looks like.
func (c retroContext) retroResult(
	ctx context.Context, r *core.Retro, writes []RepoWriteSet,
) RetroResult {
	if writes == nil {
		writes = []RepoWriteSet{}
	}
	return RetroResult{Retro: c.retroView(ctx, r), Writes: writes}
}

// Retros lists the retros of the team repository, newest first, together with
// every improvement action they left open. The open actions are the point: a
// team starting a new retro sees what it promised last time before it promises
// anything else (docs/04 section 9.1, step 7).
func (w *Workspace) Retros(ctx context.Context, p RetroListParams) (RetroListResult, error) {
	c, err := w.retroContext(ctx)
	if err != nil {
		return RetroListResult{}, err
	}
	if p.State != "" && !core.RetroState(p.State).Valid() {
		return RetroListResult{}, failf("invalid_request",
			"%q is not a retro state: collecting, voting, discussing or closed", p.State)
	}
	out := RetroListResult{
		Retros: []core.RetroSummary{}, Carried: []core.RetroActionView{},
		Diagnostics: []core.Diagnostic{},
	}
	out.Diagnostics = append(out.Diagnostics, c.retroDiags...)
	in := c.input(ctx)
	validate := c.validateRetroInput()
	var matched []*core.Retro
	for _, r := range c.retros {
		if p.Sprint != "" && r.Sprint != p.Sprint {
			continue
		}
		if p.Board != "" && r.Board != p.Board {
			continue
		}
		if p.State != "" && string(r.State) != p.State {
			continue
		}
		matched = append(matched, r)
		view := core.BuildRetroView(r, core.RetroInput{Board: in})
		out.Retros = append(out.Retros, view.Retro)
		out.Diagnostics = append(out.Diagnostics, r.Validate(validate)...)
	}
	out.Carried = core.CarriedActions(nil, matched, in)
	return out, nil
}

// Retro renders one retro: its notes, its themes ranked by votes, its actions
// with the live state of the tasks they became, and the still-open actions of
// the retros before it.
func (w *Workspace) Retro(ctx context.Context, id string) (core.RetroView, error) {
	c, err := w.retroContext(ctx)
	if err != nil {
		return core.RetroView{}, err
	}
	r, err := c.team.Vault.Retro(ctx, id)
	if err != nil {
		return core.RetroView{}, err
	}
	return c.retroView(ctx, r), nil
}

// CreateRetro writes a new retro file into the team repository, and points the
// sprint it reviews back at it. Everything the caller leaves out is derived
// from that sprint — the board, the title, the participants — because a retro
// nobody can start in one click is a retro nobody runs.
func (w *Workspace) CreateRetro(ctx context.Context, p RetroCreateParams) (RetroResult, error) {
	c, err := w.retroContext(ctx)
	if err != nil {
		return RetroResult{}, err
	}
	state := core.RetroCollecting
	if p.State != "" {
		state = core.RetroState(p.State)
		if !state.Valid() {
			return RetroResult{}, failf("invalid_request",
				"%q is not a retro state: collecting, voting, discussing or closed", p.State)
		}
	}
	var sprint *core.Sprint
	if p.Sprint != "" {
		if sprint = c.sprintOf(p.Sprint); sprint == nil {
			return RetroResult{}, failf("not_found", "no sprint %q in this team repository", p.Sprint)
		}
		if existing := sprint.Retro; existing != "" {
			return RetroResult{}, failf("conflict",
				"sprint %s already has retro %s; open it instead of starting a second one",
				sprint.ID, existing)
		}
	}
	date := core.NewDate(c.team.Vault.Now())
	if p.Date != "" {
		if date, err = core.ParseDate(p.Date); err != nil {
			return RetroResult{}, failf("invalid_request", "%v", err)
		}
	}
	id, err := c.team.Vault.NextRetroID(ctx)
	if err != nil {
		return RetroResult{}, err
	}
	retro := &core.Retro{
		ID: id, Type: "retro", Title: p.Title, Board: p.Board, Date: date,
		Facilitator: p.Facilitator, Participants: p.Participants, State: state,
		Anonymous: p.Anonymous, VotesPerPerson: p.VotesPerPerson,
		CarriedFrom: p.CarriedFrom, Author: p.Author,
		Created: core.NewTimestamp(c.team.Vault.Now()),
	}
	if sprint != nil {
		retro.Sprint = sprint.ID
		if retro.Board == "" {
			retro.Board = sprint.Board
		}
		if retro.Title == "" {
			retro.Title = sprint.DisplayTitle() + " Retrospective"
		}
		if len(retro.Participants) == 0 {
			retro.Participants = append([]string(nil), sprint.Participants...)
		}
	}
	if retro.Title == "" {
		retro.Title = fmt.Sprintf("Retrospective %s", date)
	}
	if retro.CarriedFrom == "" {
		if previous := c.previousRetro(retro); previous != nil {
			retro.CarriedFrom = previous.ID
		}
	}
	// The three collection sections exist from the start, so that the file
	// reads as a retro in a plain editor before anybody has typed a note.
	retro.SetBody("## Went well\n\n## To improve\n\n## Puzzles\n\n## Discussion\n\n## Actions")

	written, writes, err := c.team.Vault.WriteRetro(ctx, retro, "")
	if err != nil {
		return RetroResult{}, err
	}
	sets := []RepoWriteSet{teamWrites(c.team.ID, writes)}
	if sprint != nil {
		sprint.Retro = written.ID
		if _, sprintWrites, err := c.team.Vault.WriteSprint(ctx, sprint, sprint.Rev); err == nil {
			sets = append(sets, teamWrites(c.team.ID, sprintWrites))
		} else {
			return RetroResult{}, err
		}
	}
	return c.retroResult(ctx, written, sets), nil
}

// previousRetro is the most recent retro of the same board, or of the team when
// the new retro names no board.
func (c retroContext) previousRetro(r *core.Retro) *core.Retro {
	for _, candidate := range c.retros {
		if candidate.ID == r.ID {
			continue
		}
		if r.Board != "" && candidate.Board != "" && candidate.Board != r.Board {
			continue
		}
		return candidate
	}
	return nil
}

// UpdateRetro applies one session's worth of edits to a retro file: notes added
// and reclassified, themes merged, votes cast, actions selected. Every change
// is one write to the retro file in the team repository (docs/04 section 11).
func (w *Workspace) UpdateRetro(ctx context.Context, p RetroUpdateParams) (RetroResult, error) {
	c, err := w.retroContext(ctx)
	if err != nil {
		return RetroResult{}, err
	}
	retro, err := c.team.Vault.Retro(ctx, p.ID)
	if err != nil {
		return RetroResult{}, err
	}
	if err := checkRetroRev(retro, p.Rev); err != nil {
		return RetroResult{}, err
	}
	if err := applyRetroPatch(retro, p.Patch); err != nil {
		return RetroResult{}, err
	}
	written, writes, err := c.team.Vault.WriteRetro(ctx, retro, retro.Rev)
	if err != nil {
		return RetroResult{}, err
	}
	return c.retroResult(ctx, written, []RepoWriteSet{teamWrites(c.team.ID, writes)}), nil
}

// applyRetroPatch is the whole of "retro.update" that is not I/O.
func applyRetroPatch(retro *core.Retro, patch RetroPatch) error {
	if patch.Title != nil {
		retro.Title = *patch.Title
	}
	if patch.Date != nil {
		date, err := core.ParseDate(*patch.Date)
		if err != nil {
			return failf("invalid_request", "%v", err)
		}
		retro.Date = date
	}
	if patch.State != nil {
		state := core.RetroState(*patch.State)
		if !state.Valid() {
			return failf("invalid_request",
				"%q is not a retro state: collecting, voting, discussing or closed", *patch.State)
		}
		retro.State = state
	}
	if patch.Facilitator != nil {
		retro.Facilitator = *patch.Facilitator
	}
	if patch.Participants != nil {
		retro.Participants = append([]string(nil), *patch.Participants...)
	}
	if patch.Anonymous != nil {
		retro.Anonymous = *patch.Anonymous
	}
	if patch.VotesPerPerson != nil {
		budget := *patch.VotesPerPerson
		retro.VotesPerPerson = &budget
	}
	if patch.CarriedFrom != nil {
		retro.CarriedFrom = *patch.CarriedFrom
	}
	if err := applyRetroNotes(retro, patch); err != nil {
		return err
	}
	if patch.Themes != nil {
		themes := append([]core.RetroTheme(nil), *patch.Themes...)
		for i := range themes {
			if themes[i].ID == "" {
				// A theme the room merged during the session and never named.
				retro.Themes = append(retro.Themes, core.RetroTheme{ID: retro.NextThemeID()})
				themes[i].ID = retro.Themes[len(retro.Themes)-1].ID
			}
			if !core.ValidRetroLocalID(themes[i].ID) {
				return failf("invalid_request",
					"%q is not a theme id: one to sixteen of [a-z0-9-]", themes[i].ID)
			}
			if themes[i].Category != "" && !themes[i].Category.Valid() {
				return failf("invalid_request",
					"%q is not a category: went_well, to_improve or puzzle", themes[i].Category)
			}
		}
		retro.Themes = themes
	}
	if patch.Votes != nil {
		retro.Votes = map[string][]string{}
		for theme, voters := range *patch.Votes {
			retro.Votes[theme] = append([]string(nil), voters...)
		}
	}
	return applyRetroActions(retro, patch)
}

// applyRetroNotes adds, edits and removes the sticky notes of a session.
func applyRetroNotes(retro *core.Retro, patch RetroPatch) error {
	for _, draft := range patch.AddNotes {
		category := core.RetroCategory(draft.Category)
		if !category.Valid() {
			return failf("invalid_request",
				"%q is not a category: went_well, to_improve or puzzle", draft.Category)
		}
		retro.AddNote(category, draft.Text, draft.Author)
	}
	for _, edit := range patch.UpdateNotes {
		category := core.RetroCategory(edit.Category)
		if edit.Category != "" && !category.Valid() {
			return failf("invalid_request",
				"%q is not a category: went_well, to_improve or puzzle", edit.Category)
		}
		if !retro.UpdateNote(edit.ID, edit.Text, edit.Author, category) {
			return failf("not_found", "retro %s has no note %q", retro.ID, edit.ID)
		}
	}
	for _, id := range patch.RemoveNotes {
		if !retro.RemoveNote(id) {
			return failf("not_found", "retro %s has no note %q", retro.ID, id)
		}
	}
	return nil
}

// applyRetroActions adds, edits and removes the improvement actions. A promoted
// action keeps its task reference whatever else changes: the link back to the
// work is the whole point of promoting it (R-RETRO-3).
func applyRetroActions(retro *core.Retro, patch RetroPatch) error {
	for _, draft := range patch.AddActions {
		if draft.ID != "" && !core.ValidRetroLocalID(draft.ID) {
			return failf("invalid_request",
				"%q is not an action id: one to sixteen of [a-z0-9-]", draft.ID)
		}
		if _, taken := retro.Action(draft.ID); draft.ID != "" && taken {
			return failf("conflict", "retro %s already has an action %q", retro.ID, draft.ID)
		}
		action := core.RetroAction{
			ID: draft.ID, Title: draft.Title, Owner: draft.Owner,
			Theme: draft.Theme, Note: draft.Note,
		}
		if draft.Due != "" {
			due, err := core.ParseDate(draft.Due)
			if err != nil {
				return failf("invalid_request", "%v", err)
			}
			action.Due = due
		}
		retro.AddAction(action)
	}
	for _, edit := range patch.UpdateActions {
		action, ok := retro.Action(edit.ID)
		if !ok {
			return failf("not_found", "retro %s has no action %q", retro.ID, edit.ID)
		}
		if edit.Title != nil {
			action.Title = *edit.Title
		}
		if edit.Owner != nil {
			action.Owner = *edit.Owner
		}
		if edit.Theme != nil {
			action.Theme = *edit.Theme
		}
		if edit.Note != nil {
			action.Note = *edit.Note
		}
		if edit.Due != nil {
			if *edit.Due == "" {
				action.Due = core.Date{}
			} else {
				due, err := core.ParseDate(*edit.Due)
				if err != nil {
					return failf("invalid_request", "%v", err)
				}
				action.Due = due
			}
		}
		if edit.Status != nil {
			status := core.RetroActionStatus(*edit.Status)
			if !status.Valid() {
				return failf("invalid_request",
					"%q is not an action status: proposed, promoted, done or dropped", *edit.Status)
			}
			action.Status = status
		}
	}
	for _, id := range patch.RemoveActions {
		if !retro.RemoveAction(id) {
			return failf("not_found", "retro %s has no action %q", retro.ID, id)
		}
	}
	retro.Refresh()
	return nil
}

// PromoteRetroAction turns one improvement action into a task in a project
// repository and writes the reference back into the retro, so that neither end
// of the link can be lost (R-RETRO-2, R-RETRO-3).
//
// It writes to two repositories. When the target project is not cloned it
// refuses rather than half-writing: the UI then offers "copy as Markdown" so a
// human can paste the task where it belongs.
func (w *Workspace) PromoteRetroAction(ctx context.Context, p RetroPromoteParams) (RetroResult, error) {
	c, err := w.retroContext(ctx)
	if err != nil {
		return RetroResult{}, err
	}
	retro, err := c.team.Vault.Retro(ctx, p.ID)
	if err != nil {
		return RetroResult{}, err
	}
	if err := checkRetroRev(retro, p.Rev); err != nil {
		return RetroResult{}, err
	}
	action, ok := retro.Action(p.Action)
	if !ok {
		return RetroResult{}, failf("not_found", "retro %s has no action %q", retro.ID, p.Action)
	}
	if action.Task != "" {
		return RetroResult{}, failf(RetroActionPromotedCode,
			"action %s is already promoted to %s", action.ID, action.Task)
	}
	key := core.ProjectKey(p.Project)
	if key == "" {
		return RetroResult{}, failf("invalid_request", "name the project the task belongs in")
	}
	if len(c.declared) > 0 && !containsProjectKey(c.declared, key) {
		return RetroResult{}, failf("not_found",
			"project %s is not declared in %s", key, core.TeamFileName)
	}
	owner, cloned := c.owners[key]
	if !cloned {
		return RetroResult{}, failf(RetroNotClonedCode,
			"project %s is not cloned on this machine; clone it, or copy the action as Markdown", key)
	}

	plan := core.PlanPromotion(retro, *action, key, p.Labels)
	task, taskWrites, err := owner.Vault.CreateItem(ctx, key, plan.Draft)
	if err != nil {
		return RetroResult{}, err
	}
	action.Task = core.Ref{Project: key, Item: task.ID}.String()
	action.Status = core.ActionPromoted
	retro.Refresh()

	written, writes, err := c.team.Vault.WriteRetro(ctx, retro, retro.Rev)
	if err != nil {
		return RetroResult{}, err
	}
	result := c.retroResult(ctx, written, []RepoWriteSet{
		{VaultID: owner.ID, Written: taskWrites.Written, Removed: taskWrites.Removed},
		teamWrites(c.team.ID, writes),
	})
	result.Task = task
	return result, nil
}

// checkRetroRev enforces the optimistic lock of a retro write.
func checkRetroRev(r *core.Retro, rev string) error {
	if rev == "" || rev == "*" || string(r.Rev) == rev {
		return nil
	}
	return &Error{
		Code: "stale_revision", Path: r.Path, Current: string(r.Rev),
		Message: fmt.Sprintf("retro %s was modified since revision %s (current %s)", r.ID, rev, r.Rev),
	}
}

// containsProjectKey reports whether list holds key.
func containsProjectKey(list []core.ProjectKey, key core.ProjectKey) bool {
	for _, entry := range list {
		if entry == key {
			return true
		}
	}
	return false
}
