package core

import (
	"fmt"
	"sort"
)

// This file renders a retrospective: the themes ranked by the votes they got,
// the improvement actions with the live state of the tasks they were promoted
// into, and the open actions of the previous retros that the room has to look
// at before writing new ones (docs/04 section 9.1, step 7).
//
// Like boardview.go and sprintview.go it is pure: the adapters gather the
// inputs, this decides what a retro looks like, and the same decision therefore
// holds in the browser and in the companion process.

// RetroThemeView is one theme plus what the room decided about it: how many
// votes it got, the notes it absorbed and the actions it produced.
type RetroThemeView struct {
	RetroTheme
	Votes int `json:"votes"`
	// Voters are the handles that voted for the theme, sorted.
	Voters []string `json:"voters,omitempty"`
	// NoteTexts are the notes this theme grouped, resolved from the body.
	NoteTexts []RetroNote `json:"noteTexts,omitempty"`
	// Actions are the ids of the improvement actions that came out of it.
	Actions []string `json:"actions,omitempty"`
}

// RetroActionView is one improvement action plus its live state. Once the
// action has been promoted the task in the project repository is the truth and
// the retro's own `status` is only a fallback (R-RETRO-1), so the view carries
// the card the reference resolved to and grades openness from it.
type RetroActionView struct {
	RetroAction
	// Retro is the retro the action belongs to. It is what lets the carried
	// list of a new retro say where an old action came from.
	Retro string `json:"retro"`
	// RetroTitle is that retro's display title.
	RetroTitle string `json:"retroTitle,omitempty"`
	// Card is the promoted task as the workspace resolves it: live from a
	// clone, from the committed index snapshot, or unresolved with a reason.
	Card *BoardCard `json:"card,omitempty"`
	// Done reports a finished action: a promoted one whose task is in a
	// terminal status, or an unpromoted one marked `done`.
	Done bool `json:"done"`
	// Open is an action that is neither done nor dropped: exactly what the next
	// retro has to review.
	Open bool `json:"open"`
	// Reason explains an action whose task could not be read.
	Reason string `json:"reason,omitempty"`
}

// RetroSummary is a retro as an index reads it: the file's own fields plus the
// counts that say whether it was followed through.
type RetroSummary struct {
	ID           string        `json:"id"`
	Title        string        `json:"title"`
	Sprint       string        `json:"sprint,omitempty"`
	Board        string        `json:"board,omitempty"`
	Date         Date          `json:"date,omitempty"`
	Facilitator  string        `json:"facilitator,omitempty"`
	Participants []string      `json:"participants,omitempty"`
	State        RetroState    `json:"state"`
	Anonymous    bool          `json:"anonymous,omitempty"`
	VoteBudget   int           `json:"voteBudget"`
	CarriedFrom  string        `json:"carriedFrom,omitempty"`
	Notes        int           `json:"notes"`
	Themes       int           `json:"themes"`
	Metrics      RetroMetrics  `json:"metrics"`
	Actions      []RetroAction `json:"actions"`
	Path         string        `json:"path,omitempty"`
	Rev          Rev           `json:"rev,omitempty"`
}

// RetroMetrics counts the follow-through of one retro: an action list nobody
// finished is the failure mode this whole feature exists to make visible.
type RetroMetrics struct {
	Actions  int `json:"actions"`
	Promoted int `json:"promoted"`
	Done     int `json:"done"`
	Open     int `json:"open"`
	Dropped  int `json:"dropped"`
	// NoOwner counts the actions nobody is accountable for (R-RETRO-4).
	NoOwner int `json:"noOwner"`
}

// RetroView is one retro as the UI runs it: the header, the notes by category,
// the themes ranked by votes, the actions with their live task state, and the
// still-open actions of the previous retros.
type RetroView struct {
	Retro RetroSummary `json:"retro"`
	// Notes are the body bullets, in document order.
	Notes []RetroNote `json:"notes"`
	// Themes are ranked by votes descending, then by id.
	Themes []RetroThemeView `json:"themes"`
	// Actions are this retro's own improvement actions, in file order.
	Actions []RetroActionView `json:"actions"`
	// Carried are the actions of the previous retros that are still open. They
	// are shown when a new retro starts, because the point is following
	// through, not writing notes (docs/04 section 9.1, step 7).
	Carried []RetroActionView `json:"carried"`
	// Sprint is the sprint this retro reviews, when the team repository holds
	// it: enough to show what was committed against what was finished.
	Sprint      *SprintSummary `json:"sprint,omitempty"`
	Body        string         `json:"body,omitempty"`
	Diagnostics []Diagnostic   `json:"diagnostics"`
}

// SummarizeRetro builds the header and the follow-through counts of one retro.
func SummarizeRetro(r *Retro, actions []RetroActionView) RetroSummary {
	out := RetroSummary{
		ID: r.ID, Title: r.DisplayTitle(), Sprint: r.Sprint, Board: r.Board,
		Date: r.Date, Facilitator: r.Facilitator,
		Participants: append([]string(nil), r.Participants...),
		State:        r.State, Anonymous: r.Anonymous, VoteBudget: r.VoteBudget(),
		CarriedFrom: r.CarriedFrom, Notes: len(r.Notes), Themes: len(r.Themes),
		Actions: append([]RetroAction{}, r.Actions...),
		Path:    r.Path, Rev: r.Rev,
	}
	out.Metrics.Actions = len(r.Actions)
	for _, action := range actions {
		if action.Task != "" {
			out.Metrics.Promoted++
		}
		if action.Owner == "" {
			out.Metrics.NoOwner++
		}
		switch {
		case action.State() == ActionDropped:
			out.Metrics.Dropped++
		case action.Done:
			out.Metrics.Done++
		default:
			out.Metrics.Open++
		}
	}
	return out
}

// RetroInput is everything BuildRetroView needs beyond the retro itself.
type RetroInput struct {
	// Board is the render context of the repositories that happen to be open:
	// it is what resolves a promoted task, live or from a snapshot.
	Board BoardInput
	// Previous are the earlier retros of the team, newest first. Their open
	// actions are carried into the view.
	Previous []*Retro
	// Sprint is the sprint this retro reviews, when the team repository has it.
	Sprint *Sprint
	// SprintCards are the cards of that sprint's scope, for its metrics.
	SprintCards []BoardCard
}

// BuildRetroView renders one retro over the repositories that happen to be
// open. It never fails: an action whose project nobody cloned resolves from the
// committed snapshot, and one nothing can resolve carries the reason, exactly
// as a board card does (docs/04 section 7).
func BuildRetroView(r *Retro, in RetroInput) RetroView {
	out := RetroView{
		Notes:   append([]RetroNote{}, r.Notes...),
		Themes:  []RetroThemeView{},
		Actions: []RetroActionView{},
		Carried: []RetroActionView{},
		Body:    r.Body,
	}

	for _, action := range r.Actions {
		out.Actions = append(out.Actions, resolveRetroAction(r, action, in.Board))
	}

	byTheme := map[string][]string{}
	for _, action := range r.Actions {
		if action.Theme != "" {
			byTheme[action.Theme] = append(byTheme[action.Theme], action.ID)
		}
	}
	notes := map[string]RetroNote{}
	for _, note := range r.Notes {
		if note.ID != "" {
			notes[note.ID] = note
		}
	}
	for _, theme := range r.Themes {
		view := RetroThemeView{RetroTheme: theme, Votes: r.VoteCount(theme.ID), Actions: byTheme[theme.ID]}
		view.Voters = append([]string(nil), r.Votes[theme.ID]...)
		sort.Strings(view.Voters)
		for _, id := range theme.Notes {
			if note, ok := notes[id]; ok {
				view.NoteTexts = append(view.NoteTexts, note)
			}
		}
		out.Themes = append(out.Themes, view)
	}
	sort.SliceStable(out.Themes, func(i, j int) bool {
		if out.Themes[i].Votes != out.Themes[j].Votes {
			return out.Themes[i].Votes > out.Themes[j].Votes
		}
		return out.Themes[i].ID < out.Themes[j].ID
	})

	out.Carried = CarriedActions(r, in.Previous, in.Board)

	if in.Sprint != nil {
		summary := SummarizeSprint(in.Sprint, in.SprintCards, in.Board.Now)
		out.Sprint = &summary
	}
	out.Retro = SummarizeRetro(r, out.Actions)
	out.Diagnostics = []Diagnostic{}
	return out
}

// CarriedActions returns the still-open improvement actions of the retros that
// came before this one, newest retro first. `carried_from` narrows the search
// to one predecessor chain; without it every earlier retro is reviewed, which
// is what a team that never set the field should still see.
func CarriedActions(r *Retro, previous []*Retro, in BoardInput) []RetroActionView {
	out := []RetroActionView{}
	for _, earlier := range previous {
		if earlier == nil || (r != nil && earlier.ID == r.ID) {
			continue
		}
		if r != nil && r.CarriedFrom != "" && earlier.ID != r.CarriedFrom {
			continue
		}
		for _, action := range earlier.Actions {
			view := resolveRetroAction(earlier, action, in)
			if !view.Open {
				continue
			}
			out = append(out, view)
		}
	}
	return out
}

// resolveRetroAction grades one action against the task it was promoted into.
func resolveRetroAction(r *Retro, action RetroAction, in BoardInput) RetroActionView {
	view := RetroActionView{RetroAction: action, Retro: r.ID, RetroTitle: r.DisplayTitle()}
	if action.Task != "" {
		card := ResolveCard(in, action.Task)
		view.Card = &card
		view.Reason = card.Reason
	}
	switch {
	case action.State() == ActionDropped:
		view.Done = false
	case view.Card != nil && view.Card.Status != "":
		// R-RETRO-1: once the action has a task, that task decides.
		view.Done = view.Card.Done()
	default:
		view.Done = action.State() == ActionDone
	}
	view.Open = !view.Done && action.State() != ActionDropped
	return view
}

// ResolveCard renders one `<projectKey>/<itemId>` reference as a card over the
// repositories that happen to be open: live from a clone, read-only from the
// committed index snapshot, or unresolved carrying the reason. It is the
// single-reference form of what BuildBoardView does for a whole board, and it
// is what a retro action, and any other lone reference, is displayed through.
func ResolveCard(in BoardInput, raw string) BoardCard {
	card := BoardCard{Ref: raw}
	ref, err := ParseRef(raw)
	if err != nil {
		card.Reason = err.Error()
		return card
	}
	card.Project, card.Item = ref.Project, ref.Item
	card.Declared = containsKey(in.Declared, ref.Project)
	for _, source := range in.Sources {
		if source.Project != ref.Project {
			continue
		}
		for i := range source.Items {
			it := &source.Items[i]
			if it.ID != ref.Item {
				continue
			}
			found := cardOf(ref.Project, source.VaultID, it)
			found.Declared = card.Declared
			if source.Config != nil {
				found.Category = source.Config.CategoryOf(it.Status)
			}
			return found
		}
		card.Reason = fmt.Sprintf("%s does not exist in the clone of %s", ref.Item, ref.Project)
		return card
	}
	card.Remote = true
	if entry, ok := in.Snapshots.Item(ref.Project, ref.Item); ok {
		info := in.Snapshots.Info(ref.Project)
		found := snapshotCardOf(ref.Project, entry, info, in.project(ref.Project))
		found.Declared = card.Declared
		found.Category = entry.Category
		if found.Category == "" {
			if cfg := in.Snapshots.Config(ref.Project); cfg != nil {
				found.Category = cfg.CategoryOf(entry.Status)
			}
		}
		return found
	}
	if !card.Declared {
		card.Reason = fmt.Sprintf("project %s is not declared in %s", ref.Project, TeamFileName)
		return card
	}
	card.Reason = fmt.Sprintf(
		"project %s is not cloned on this machine and its index snapshot does not carry %s",
		ref.Project, ref.Item)
	return card
}

// PromoteAction is the plan of turning one improvement action into a task in a
// project repository (R-RETRO-2, R-RETRO-3). It is computed here so that the
// browser and the companion produce byte-identical tasks; the caller does the
// writing, because only it knows which repository is open.
type PromoteAction struct {
	// Project is the repository the task is created in.
	Project ProjectKey
	// Draft is the task as it must be written.
	Draft ItemDraft
}

// RetroTaskLabel is the label a promoted task carries, so that the work a
// retrospective produced is findable as a class (R-RETRO-3).
const RetroTaskLabel = "retro"

// PlanPromotion builds the task one improvement action becomes. The link back
// to the retro is a line of body text rather than a wikilink, because team-repo
// pages are not addressable from a project repository (R-RETRO-3).
func PlanPromotion(r *Retro, action RetroAction, project ProjectKey, labels []string) PromoteAction {
	if len(labels) == 0 {
		labels = []string{RetroTaskLabel}
	}
	draft := ItemDraft{
		Type:   TypeTask,
		Title:  action.Title,
		Author: r.Facilitator,
		Labels: append([]string(nil), labels...),
		Due:    action.Due,
		Body:   PromotionBody(r, action),
	}
	if action.Owner != "" {
		draft.Assignees = []string{action.Owner}
	}
	return PromoteAction{Project: project, Draft: draft}
}

// PromotionBody is the body of a promoted task: what the action said, then the
// sentence that says where it came from.
func PromotionBody(r *Retro, action RetroAction) string {
	body := "## Description\n\n"
	if action.Note != "" {
		body += action.Note + "\n\n"
	}
	body += fmt.Sprintf("Promoted from retro %s (action %s).\n", r.ID, action.ID)
	return body
}
