package mcp

import (
	"context"
	"strings"

	"github.com/digiogithub/git-in-track/internal/core"
)

// The sprint rollover tools: close a sprint and move what is unfinished
// somewhere else (story GIT-US-0085, docs/04 R-SPR-3 and R-SPR-8).
//
// Both are deliberately previewable. `dryRun` computes the whole report and
// writes nothing, which is what the confirmation dialog in the web UI renders
// and what an agent should ask for before it moves anyone's work.

// ------------------------------------------------------------------ input ---

// SprintCarryDecision overrides the bulk mode for one reference.
type SprintCarryDecision struct {
	Ref    string `json:"ref" jsonschema:"Card reference as <projectKey>/<itemId>, for example ACME/ACME-T-0311"`
	Action string `json:"action" jsonschema:"leave, next or backlog"`
	Sprint string `json:"sprint,omitempty" jsonschema:"next only: the sprint to carry into"`
	Status string `json:"status,omitempty" jsonschema:"backlog only: the status to return the item to"`
}

// CloseSprintInput closes one sprint and optionally moves what it did not
// finish.
type CloseSprintInput struct {
	Team string `json:"team,omitempty" jsonschema:"Team repository id; required when more than one is open"`
	ID   string `json:"id" jsonschema:"Sprint id, for example ACME-TEAM-S-0007"`
	Rev  string `json:"rev" jsonschema:"Required. The sprint rev from the read this close is based on; \"*\" closes whatever is there now"`
	// Mode is the bulk decision. Absent, nothing is moved: closing a sprint
	// modifies no item unless the caller chose it (R-SPR-3).
	Mode   string `json:"mode,omitempty" jsonschema:"next, backlog or none; default none, which moves nothing"`
	Target string `json:"target,omitempty" jsonschema:"next only: the sprint to move into; empty picks the earliest planned sprint of the same board"`
	// Carry overrides the bulk mode for the references it names.
	Carry []SprintCarryDecision `json:"carry,omitempty" jsonschema:"Per-reference decisions, which win over mode"`
	// DryRun is the answer to "what would this do?". Ask for it first.
	DryRun bool `json:"dryRun,omitempty" jsonschema:"Compute the report and write nothing. Call with true first, show the counts, and only then repeat with false"`
}

// TransferSprintItemsInput moves the unfinished work of one sprint somewhere
// else without closing anything.
type TransferSprintItemsInput struct {
	Team   string                `json:"team,omitempty" jsonschema:"Team repository id; required when more than one is open"`
	ID     string                `json:"id" jsonschema:"Sprint id to move work out of"`
	Rev    string                `json:"rev" jsonschema:"Required. The sprint rev from the read this transfer is based on; \"*\" transfers against whatever is there now"`
	Mode   string                `json:"mode,omitempty" jsonschema:"next or backlog; default next"`
	Target string                `json:"target,omitempty" jsonschema:"next only: the sprint to move into"`
	Carry  []SprintCarryDecision `json:"carry,omitempty" jsonschema:"Per-reference decisions, which win over mode"`
	DryRun bool                  `json:"dryRun,omitempty" jsonschema:"Compute the report and write nothing. Call with true first"`
}

// ----------------------------------------------------------------- output ---

// SprintReport is what a close or a transfer answers with: the counts, the
// destination and every refusal, which is exactly what a confirmation dialog
// shows and what an agent needs to decide whether to commit.
type SprintReport struct {
	Sprint string `json:"sprint"`
	Board  string `json:"board,omitempty"`
	State  string `json:"state,omitempty" jsonschema:"The sprint state after the call"`
	Rev    string `json:"rev,omitempty" jsonschema:"Sprint rev after the call; quote it on the next write"`
	// DryRun marks a preview: nothing was written.
	DryRun bool `json:"dryRun,omitempty"`

	Completed  int `json:"completed" jsonschema:"References the sprint finished"`
	Incomplete int `json:"incomplete" jsonschema:"References it did not finish"`
	Unresolved int `json:"unresolved" jsonschema:"References nothing could grade; never moved"`
	Moved      int `json:"moved" jsonschema:"Decisions that were applied, or would be"`
	Failed     int `json:"failed" jsonschema:"Decisions that could not be applied"`

	CompletedPoints  float64 `json:"completedPoints,omitempty"`
	IncompletePoints float64 `json:"incompletePoints,omitempty"`

	// Carried lists every decision, refusals included, one line each.
	Carried []CarryOutcome `json:"carried,omitempty"`
	Changed []string       `json:"changed,omitempty" jsonschema:"Vault-relative paths written, across every repository"`
}

// CarryOutcome is one decision and what became of it.
type CarryOutcome struct {
	Ref    string `json:"ref"`
	Action string `json:"action"`
	Sprint string `json:"sprint,omitempty" jsonschema:"The sprint the reference went into"`
	Status string `json:"status,omitempty" jsonschema:"The status the item was returned to"`
	// Error explains a decision that could not be applied. The rest of the
	// operation still went through (R-SPR-8).
	Error string `json:"error,omitempty"`
}

// ---------------------------------------------------------------- registry --

// registerSprintTools declares the sprint rollover half of the surface.
func registerSprintTools(s *Server) {
	register(s, toolDef{
		Name:  "close_sprint",
		Title: "Close a sprint and roll its work over",
		Description: "Close one sprint and, optionally, move everything it did not finish into " +
			"another sprint or back to the backlog in the same operation. Finished work is never " +
			"touched, and nothing moves unless mode or carry says so. " +
			"Call it with dryRun true first: that returns the full report — how many are done, how " +
			"many are not, where each would go and every refusal — and writes nothing. Show those " +
			"counts before you repeat the call with dryRun false. rev is required: quote the sprint " +
			"rev the read this close is based on returned.",
		Write: true,
	}, closeSprint)

	register(s, toolDef{
		Name:  "transfer_sprint_items",
		Title: "Move unfinished sprint work elsewhere",
		Description: "Move the unfinished references of one sprint into another sprint, or back to " +
			"each item's own backlog, without closing either sprint. A target whose derived status " +
			"is completed is refused: its numbers are history. An item whose project is not cloned " +
			"cannot be returned to a backlog and is reported on its own line while the rest goes " +
			"through. " +
			"Call it with dryRun true first to see what would move; only then repeat with false.",
		Write: true,
	}, transferSprintItems)
}

// ---------------------------------------------------------------- handlers --

// closeSprint closes one sprint through the workspace, which owns the ordering
// of the writes across the team repository and the project clones.
func closeSprint(ctx context.Context, s *Server, in CloseSprintInput) (SprintReport, error) {
	if strings.TrimSpace(in.ID) == "" {
		return SprintReport{}, invalidField("id", "close_sprint needs a sprint id", "ACME-TEAM-S-0007")
	}
	rev, err := requiredRev("rev", in.Rev)
	if err != nil {
		return SprintReport{}, err
	}
	params := map[string]any{
		"team": in.Team, "id": in.ID, "rev": rev,
		"carry": carryParams(in.Carry), "dryRun": in.DryRun,
	}
	if mode := strings.TrimSpace(in.Mode); mode != "" {
		params["transfer"] = map[string]any{"mode": mode, "target": in.Target}
	}
	return sprintWrite(ctx, s, "close_sprint", "sprint.close", params, in.DryRun)
}

// transferSprintItems moves unfinished work without closing anything.
func transferSprintItems(ctx context.Context, s *Server, in TransferSprintItemsInput) (SprintReport, error) {
	if strings.TrimSpace(in.ID) == "" {
		return SprintReport{}, invalidField("id", "transfer_sprint_items needs a sprint id",
			"ACME-TEAM-S-0007")
	}
	rev, err := requiredRev("rev", in.Rev)
	if err != nil {
		return SprintReport{}, err
	}
	params := map[string]any{
		"team": in.Team, "id": in.ID, "rev": rev,
		"mode": in.Mode, "target": in.Target,
		"carry": carryParams(in.Carry), "dryRun": in.DryRun,
	}
	return sprintWrite(ctx, s, "transfer_sprint_items", "sprint.transfer", params, in.DryRun)
}

// sprintWrite dispatches one rollover method and projects its report. A dry run
// is never announced: nothing changed, so there is nothing for the host to
// commit or to publish.
func sprintWrite(
	ctx context.Context, s *Server, tool, method string, params map[string]any, dry bool,
) (SprintReport, error) {
	result, err := s.dispatchRaw(ctx, method, params)
	if err != nil {
		return SprintReport{}, err
	}
	out, err := sprintReportOf(result)
	if err != nil {
		return SprintReport{}, err
	}
	if !dry {
		s.announce(ctx, WriteEvent{Tool: tool, Method: method, Op: "moved", Result: result})
	}
	return out, nil
}

// carryParams turns the tool's per-reference decisions into the core's shape.
func carryParams(decisions []SprintCarryDecision) []map[string]any {
	out := make([]map[string]any, 0, len(decisions))
	for _, d := range decisions {
		out = append(out, map[string]any{
			"ref": d.Ref, "action": d.Action, "sprint": d.Sprint, "status": d.Status,
		})
	}
	return out
}

// sprintReportOf projects the core's answer onto the compact report. The counts
// are derived from the lists the core produced rather than trusted from a
// separate field, so they cannot drift from what is listed.
func sprintReportOf(result any) (SprintReport, error) {
	payload, err := decodeResult[struct {
		Sprint struct {
			Sprint struct {
				ID    string `json:"id"`
				Board string `json:"board"`
				State string `json:"state"`
				Rev   string `json:"rev"`
			} `json:"sprint"`
		} `json:"sprint"`
		Report *core.SprintCloseReport `json:"report"`
		Writes []writeSet              `json:"writes"`
		DryRun bool                    `json:"dryRun"`
	}](result)
	if err != nil {
		return SprintReport{}, err
	}
	summary := payload.Sprint.Sprint
	out := SprintReport{
		Sprint: summary.ID, Board: summary.Board, State: summary.State,
		Rev: summary.Rev, DryRun: payload.DryRun,
	}
	if payload.Report != nil {
		out.Completed = len(payload.Report.Completed)
		out.Incomplete = len(payload.Report.Incomplete)
		out.Unresolved = len(payload.Report.Unresolved)
		out.CompletedPoints = payload.Report.CompletedPoints
		out.IncompletePoints = payload.Report.IncompletePoints
		for _, carried := range payload.Report.Carried {
			if carried.Error != "" {
				out.Failed++
			} else if carried.Action != core.CarryLeave {
				out.Moved++
			}
			out.Carried = append(out.Carried, CarryOutcome{
				Ref: carried.Ref, Action: string(carried.Action), Sprint: carried.Sprint,
				Status: string(carried.Status), Error: carried.Error,
			})
		}
	}
	for _, set := range payload.Writes {
		out.Changed = append(out.Changed, set.paths()...)
	}
	return out, nil
}
