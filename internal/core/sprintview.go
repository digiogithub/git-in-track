package core

import (
	"fmt"
	"time"
)

// This file renders a sprint: the header a scrum board shows (goal, dates,
// remaining days, committed versus completed points) and the planning view that
// moves items in and out of the scope.
//
// Like boardview.go it is pure: the adapters gather the inputs, this decides
// what the sprint looks like, and the same decision therefore holds in the
// browser and in the companion process.

// SprintMetrics is what a sprint header and a closing report count
// (docs/04 section 8, docs/05 section 9).
type SprintMetrics struct {
	// Items is the size of the scope, whether or not each reference resolved.
	Items int `json:"items"`
	// Resolved is how many of them a clone or a snapshot could render.
	Resolved int `json:"resolved"`
	// Done is how many of the resolved cards sit in a terminal status.
	Done int `json:"done"`
	// Points is the estimate of the whole scope, CommittedPoints the estimate
	// of the references the sprint committed to, DonePoints the estimate of the
	// finished ones.
	Points          float64 `json:"points"`
	CommittedPoints float64 `json:"committedPoints"`
	DonePoints      float64 `json:"donePoints"`
	// Added counts the references that are in the scope but not in the
	// commitment: work pulled in after the sprint started (R-SPR-1).
	Added int `json:"added"`
	// Unresolved counts the references no clone and no snapshot could render.
	Unresolved int `json:"unresolved"`
}

// SprintSummary is a sprint as the UI reads it: the file's own fields plus the
// numbers derived from the cards it currently resolves to.
type SprintSummary struct {
	ID             string      `json:"id"`
	Title          string      `json:"title"`
	Board          string      `json:"board"`
	State          SprintState `json:"state"`
	Start          Date        `json:"start,omitempty"`
	End            Date        `json:"end,omitempty"`
	Goal           string      `json:"goal,omitempty"`
	CapacityHours  *float64    `json:"capacityHours,omitempty"`
	VelocityTarget *float64    `json:"velocityTarget,omitempty"`
	Participants   []string    `json:"participants,omitempty"`
	Retro          string      `json:"retro,omitempty"`
	Items          []string    `json:"items"`
	Committed      []string    `json:"committed,omitempty"`
	// TotalDays and RemainingDays are both ends inclusive; RemainingDays is 0
	// once the end date has passed.
	TotalDays     int           `json:"totalDays"`
	RemainingDays int           `json:"remainingDays"`
	Metrics       SprintMetrics `json:"metrics"`
	Body          string        `json:"body,omitempty"`
	Path          string        `json:"path,omitempty"`
	Rev           Rev           `json:"rev,omitempty"`
}

// SummarizeSprint builds the header of a sprint over the cards a board — or a
// planning view — resolved for it. cards may hold cards that are not in the
// scope; only the ones the sprint lists are counted.
func SummarizeSprint(s *Sprint, cards []BoardCard, now time.Time) SprintSummary {
	out := SprintSummary{
		ID: s.ID, Title: s.DisplayTitle(), Board: s.Board, State: s.State,
		Start: s.Start, End: s.End, Goal: s.Goal,
		CapacityHours: clonePtr(s.CapacityHours), VelocityTarget: clonePtr(s.VelocityTarget),
		Participants: append([]string(nil), s.Participants...),
		Retro:        s.Retro,
		Items:        append([]string(nil), s.Items...),
		Committed:    append([]string(nil), s.Committed...),
		TotalDays:    s.TotalDays(), RemainingDays: s.RemainingDays(now),
		Body: s.Body, Path: s.Path, Rev: s.Rev,
	}
	members := s.Members()
	committed := map[string]bool{}
	for _, ref := range s.Committed {
		committed[ref] = true
	}
	// A commitment exists once the sprint has started, even when it started
	// empty; before that everything in the scope is still being planned, so
	// nothing counts as an addition (R-SPR-1).
	started := s.State != SprintPlanned || len(s.Committed) > 0
	out.Metrics.Items = len(s.Items)
	for _, ref := range s.Items {
		if started && !committed[ref] {
			out.Metrics.Added++
		}
	}
	seen := map[string]bool{}
	for _, card := range cards {
		if !members[card.Ref] || seen[card.Ref] {
			continue
		}
		seen[card.Ref] = true
		if card.Status == "" {
			// A reference neither a clone nor a snapshot could render: it is
			// counted as unresolved, never as work.
			continue
		}
		out.Metrics.Resolved++
		out.Metrics.Points += card.Points()
		if committed[card.Ref] {
			out.Metrics.CommittedPoints += card.Points()
		}
		if card.Done() {
			out.Metrics.Done++
			out.Metrics.DonePoints += card.Points()
		}
	}
	out.Metrics.Unresolved = out.Metrics.Items - out.Metrics.Resolved
	return out
}

// SprintView is the planning view of one sprint: the scope, the candidates the
// board's filters match that the sprint does not list yet, and the findings of
// the references that resolve to nothing.
type SprintView struct {
	Sprint SprintSummary `json:"sprint"`
	// Cards is the scope, in the order the sprint file lists it. A reference
	// nothing could resolve still gets a card, carrying the reason.
	Cards []BoardCard `json:"cards"`
	// Backlog is the sprint candidates: what the board would show that the
	// sprint does not list (docs/04 section 5.5).
	Backlog     []BoardCard  `json:"backlog"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

// BuildSprintView renders a sprint over the repositories that happen to be
// open. board may be nil — a sprint whose board is missing still lists its
// scope — in which case there are no candidates to offer.
//
// It never fails: a reference into a project nobody cloned resolves from the
// committed snapshot, and one nothing can resolve becomes a card carrying the
// reason, exactly as it does on a board (docs/04 section 7).
func BuildSprintView(s *Sprint, board *Board, in BoardInput) SprintView {
	out := SprintView{Cards: []BoardCard{}, Backlog: []BoardCard{}, Diagnostics: []Diagnostic{}}
	members := s.Members()
	committed := map[string]bool{}
	for _, ref := range s.Committed {
		committed[ref] = true
	}

	resolved := map[string]BoardCard{}
	if board != nil {
		walkCards(board, in, func(c cardCandidate) {
			card := c.Card
			if members[card.Ref] {
				card.InSprint = true
				card.Committed = committed[card.Ref]
				resolved[card.Ref] = card
				return
			}
			if !isSprintCandidate(board, c) {
				return
			}
			card.Backlog = true
			out.Backlog = append(out.Backlog, card)
		})
	}
	sortBoardCardsByRank(out.Backlog)

	for _, raw := range s.Items {
		if card, ok := resolved[raw]; ok {
			out.Cards = append(out.Cards, card)
			continue
		}
		card := BoardCard{Ref: raw, InSprint: true, Committed: committed[raw]}
		ref, err := ParseRef(raw)
		if err != nil {
			card.Reason = err.Error()
			out.Diagnostics = append(out.Diagnostics, Diagnostic{
				Code: CodeBoardRefFormat, Severity: SeverityWarning, Path: s.Path,
				Field: "items", Message: err.Error(),
			})
			out.Cards = append(out.Cards, card)
			continue
		}
		card.Project, card.Item = ref.Project, ref.Item
		card.Declared = containsKey(in.Declared, ref.Project)
		card.Remote = !servesProject(in, ref.Project)
		switch {
		case !card.Declared:
			card.Reason = fmt.Sprintf("project %s is not declared in %s", ref.Project, TeamFileName)
			out.Diagnostics = append(out.Diagnostics, Diagnostic{
				Code: CodeSprintRefUnknownProject, Severity: SeverityWarning, Path: s.Path,
				Field: "items", Message: card.Reason,
			})
		case !card.Remote:
			card.Reason = fmt.Sprintf("%s does not exist in the clone of %s", ref.Item, ref.Project)
			out.Diagnostics = append(out.Diagnostics, Diagnostic{
				Code: CodeSprintRefDead, Severity: SeverityWarning, Path: s.Path,
				Field: "items", Message: card.Reason,
			})
		default:
			info := in.Snapshots.Info(ref.Project)
			card.SnapshotAt = info.Generated
			card.Stale = info.Stale
			card.Reason = fmt.Sprintf(
				"project %s is not cloned on this machine and its index snapshot does not carry %s; the sprint keeps the reference",
				ref.Project, ref.Item)
		}
		out.Cards = append(out.Cards, card)
	}

	out.Sprint = SummarizeSprint(s, out.Cards, in.Now)
	return out
}

// isSprintCandidate reports whether an item the sprint does not list belongs in
// the backlog drawer: the board's filters keep it, it is not finished, and the
// backlog column's own status mapping claims it — a board that declares no
// backlog column offers everything any of its columns would show.
func isSprintCandidate(b *Board, c cardCandidate) bool {
	if !c.Matched || c.Card.Done() {
		return false
	}
	if column, ok := b.Column(b.BacklogColumn); ok {
		return column.Shows(c.Card.Project, c.Config, c.Card.Status)
	}
	for _, column := range b.Columns {
		if column.Shows(c.Card.Project, c.Config, c.Card.Status) {
			return true
		}
	}
	return false
}

// servesProject reports whether an open repository serves a project key.
func servesProject(in BoardInput, key ProjectKey) bool {
	for _, s := range in.Sources {
		if s.Project == key {
			return true
		}
	}
	return false
}

// SprintCarryAction is what happens to one unfinished item when a sprint is
// closed (R-SPR-3). Nothing happens implicitly: the caller decides per item.
type SprintCarryAction string

// The three closing choices of docs/04 R-SPR-3.
const (
	// CarryLeave leaves the item exactly where it is.
	CarryLeave SprintCarryAction = "leave"
	// CarryNext adds the reference to another sprint of the same board, a
	// write in the team repository.
	CarryNext SprintCarryAction = "next"
	// CarryBacklog sends the item back to the backlog, a status write in its
	// own project repository.
	CarryBacklog SprintCarryAction = "backlog"
)

// Valid reports whether a is one of the known closing choices.
func (a SprintCarryAction) Valid() bool {
	return a == CarryLeave || a == CarryNext || a == CarryBacklog
}

// SprintCloseReport is what closing a sprint summarized: which references were
// finished, which were not, and what was decided about each unfinished one.
type SprintCloseReport struct {
	Sprint     string      `json:"sprint"`
	Board      string      `json:"board"`
	Completed  []BoardCard `json:"completed"`
	Incomplete []BoardCard `json:"incomplete"`
	// Unresolved holds the references neither a clone nor a snapshot could
	// grade; they are reported, never counted as done.
	Unresolved       []BoardCard   `json:"unresolved"`
	CompletedPoints  float64       `json:"completedPoints"`
	IncompletePoints float64       `json:"incompletePoints"`
	Metrics          SprintMetrics `json:"metrics"`
	// Carried lists what was done with each unfinished item.
	Carried []SprintCarryResult `json:"carried"`
}

// SprintCarryResult is the outcome of one closing decision.
type SprintCarryResult struct {
	Ref    string            `json:"ref"`
	Action SprintCarryAction `json:"action"`
	// Sprint is the sprint the reference was carried into, for `next`.
	Sprint string `json:"sprint,omitempty"`
	// Status is the status the item was returned to, for `backlog`.
	Status Status `json:"status,omitempty"`
	// Error explains a decision that could not be applied; the rest of the
	// closing still went through.
	Error string `json:"error,omitempty"`
}

// SummarizeClose splits the scope of a sprint into what was finished and what
// was not. It writes nothing: closing a sprint modifies no item (R-SPR-3).
func SummarizeClose(s *Sprint, view SprintView) SprintCloseReport {
	report := SprintCloseReport{
		Sprint: s.ID, Board: s.Board,
		Completed: []BoardCard{}, Incomplete: []BoardCard{}, Unresolved: []BoardCard{},
		Carried: []SprintCarryResult{},
		Metrics: view.Sprint.Metrics,
	}
	for _, card := range view.Cards {
		switch {
		case card.Status == "":
			report.Unresolved = append(report.Unresolved, card)
		case card.Done():
			report.Completed = append(report.Completed, card)
			report.CompletedPoints += card.Points()
		default:
			report.Incomplete = append(report.Incomplete, card)
			report.IncompletePoints += card.Points()
		}
	}
	return report
}

// BacklogStatus returns the status an item goes back to when a closing sends it
// to the backlog: the first `todo` status of its own workflow, or the initial
// status when the workflow declares none.
func BacklogStatus(cfg *ProjectConfig) Status {
	if cfg == nil {
		return ""
	}
	for _, def := range cfg.Workflow.Statuses {
		if def.Category == CategoryTodo {
			return def.ID
		}
	}
	return cfg.InitialStatus()
}
