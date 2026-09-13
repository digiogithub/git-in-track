package main

import (
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/digiogithub/git-in-track/cmd/gintrack/output"
	"github.com/digiogithub/git-in-track/internal/core"
	corevault "github.com/digiogithub/git-in-track/internal/vault"
)

// This file is the `gintrack sprint` command tree (GIT-US-0092, GIT-T-0172 and
// GIT-T-0178). Like every other command file it owns no business logic: it
// resolves the configuration, mounts the workspace, calls one of the
// "sprint.*" methods of internal/vault and prints the answer. What a derived
// status is, whether a sprint may start, what closing one reports and what a
// transfer moves are all decisions of internal/core.

// --------------------------------------------------------------- payloads --

// sprintRowPayload is one sprint as `--json` reports it: the file's own fields,
// the status derived from the dates and the clock, and the metrics of the cards
// it currently resolves to.
type sprintRowPayload struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Board string `json:"board"`
	// State is the stored lifecycle field: planned, active or closed.
	State string `json:"state"`
	// Status is derived on every read and never written: draft, upcoming,
	// current or completed.
	Status          string   `json:"status"`
	Start           string   `json:"start,omitempty"`
	End             string   `json:"end,omitempty"`
	Goal            string   `json:"goal,omitempty"`
	TotalDays       int      `json:"totalDays"`
	RemainingDays   int      `json:"remainingDays"`
	Items           int      `json:"items"`
	Resolved        int      `json:"resolved"`
	Done            int      `json:"done"`
	Added           int      `json:"added"`
	Unresolved      int      `json:"unresolved"`
	Points          float64  `json:"points"`
	CommittedPoints float64  `json:"committedPoints"`
	DonePoints      float64  `json:"donePoints"`
	Participants    []string `json:"participants,omitempty"`
	Retro           string   `json:"retro,omitempty"`
	// Snapshot is the progress frozen into the file when the sprint was closed,
	// absent while it is still open.
	Snapshot *core.SprintSnapshot `json:"snapshot,omitempty"`
	Path     string               `json:"path,omitempty"`
	Rev      string               `json:"rev,omitempty"`
}

// sprintListPayload is what `gintrack sprint list --json` prints. The sprints
// come in group order — draft, upcoming, current, completed — and each row
// carries its own derived status, so the grouping the table shows is
// recoverable without repeating a sprint in two places.
type sprintListPayload struct {
	Sprints []sprintRowPayload `json:"sprints"`
	Counts  map[string]int     `json:"counts"`
	Total   int                `json:"total"`
}

// sprintCardPayload is one reference of a sprint scope or of a close report.
type sprintCardPayload struct {
	Ref       string   `json:"ref"`
	Project   string   `json:"project,omitempty"`
	Item      string   `json:"item,omitempty"`
	Title     string   `json:"title,omitempty"`
	Type      string   `json:"type,omitempty"`
	Status    string   `json:"status,omitempty"`
	Category  string   `json:"category,omitempty"`
	Assignees []string `json:"assignees,omitempty"`
	Points    float64  `json:"points"`
	Committed bool     `json:"committed"`
	Done      bool     `json:"done"`
	// Source is where the card was read from: "live" for a local clone,
	// "snapshot" for a committed index snapshot, empty when nothing resolved it.
	Source string `json:"source,omitempty"`
	// Reason explains a reference nothing could resolve.
	Reason string `json:"reason,omitempty"`
}

// sprintShowPayload is what `gintrack sprint show --json` prints.
type sprintShowPayload struct {
	Sprint      sprintRowPayload    `json:"sprint"`
	Cards       []sprintCardPayload `json:"cards"`
	Diagnostics []string            `json:"diagnostics,omitempty"`
}

// sprintCarryPayload is the outcome of one closing or transfer decision. Error
// is the per-item refusal: the rest of the operation still went through.
type sprintCarryPayload struct {
	Ref    string `json:"ref"`
	Action string `json:"action"`
	Sprint string `json:"sprint,omitempty"`
	Status string `json:"status,omitempty"`
	Error  string `json:"error,omitempty"`
}

// sprintReportPayload is the close report: what was finished, what was not, and
// what was decided about each unfinished reference.
type sprintReportPayload struct {
	Sprint           string               `json:"sprint"`
	Board            string               `json:"board"`
	Completed        []sprintCardPayload  `json:"completed"`
	Incomplete       []sprintCardPayload  `json:"incomplete"`
	Unresolved       []sprintCardPayload  `json:"unresolved"`
	CompletedPoints  float64              `json:"completedPoints"`
	IncompletePoints float64              `json:"incompletePoints"`
	Carried          []sprintCarryPayload `json:"carried"`
}

// sprintWritePayload is what the three writing subcommands print with `--json`.
type sprintWritePayload struct {
	Sprint sprintRowPayload     `json:"sprint"`
	Report *sprintReportPayload `json:"report,omitempty"`
	// DryRun marks a report computed without writing anything, so that a
	// preview can never be mistaken for a commitment.
	DryRun  bool                `json:"dryRun"`
	Written []sprintWrittenFile `json:"written"`
	Board   *sprintBoardPayload `json:"board,omitempty"`
}

// sprintWrittenFile is one file an operation saved.
type sprintWrittenFile struct {
	Repo string `json:"repo"`
	Path string `json:"path"`
}

// sprintBoardPayload names the board a start repointed at its sprint.
type sprintBoardPayload struct {
	ID     string `json:"id"`
	Sprint string `json:"sprint"`
}

// newSprintRow renders one sprint summary.
func newSprintRow(s core.SprintSummary) sprintRowPayload {
	return sprintRowPayload{
		ID: s.ID, Title: s.Title, Board: s.Board,
		State: string(s.State), Status: string(s.Status),
		Start: s.Start.String(), End: s.End.String(), Goal: s.Goal,
		TotalDays: s.TotalDays, RemainingDays: s.RemainingDays,
		Items:           s.Metrics.Items,
		Resolved:        s.Metrics.Resolved,
		Done:            s.Metrics.Done,
		Added:           s.Metrics.Added,
		Unresolved:      s.Metrics.Unresolved,
		Points:          s.Metrics.Points,
		CommittedPoints: s.Metrics.CommittedPoints,
		DonePoints:      s.Metrics.DonePoints,
		Participants:    s.Participants,
		Retro:           s.Retro,
		Snapshot:        s.Snapshot,
		Path:            s.Path,
		Rev:             string(s.Rev),
	}
}

// newSprintCard renders one board card of a scope or of a report.
func newSprintCard(c core.BoardCard) sprintCardPayload {
	return sprintCardPayload{
		Ref: c.Ref, Project: string(c.Project), Item: string(c.Item),
		Title: c.Title, Type: string(c.Type), Status: string(c.Status),
		Category: string(c.Category), Assignees: c.Assignees,
		Points: c.Points(), Committed: c.Committed, Done: c.Done(),
		Source: c.Source, Reason: c.Reason,
	}
}

// newSprintCards renders a list of cards.
func newSprintCards(cards []core.BoardCard) []sprintCardPayload {
	out := make([]sprintCardPayload, 0, len(cards))
	for _, c := range cards {
		out = append(out, newSprintCard(c))
	}
	return out
}

// newSprintReport renders a close report.
func newSprintReport(r core.SprintCloseReport) sprintReportPayload {
	out := sprintReportPayload{
		Sprint: r.Sprint, Board: r.Board,
		Completed:       newSprintCards(r.Completed),
		Incomplete:      newSprintCards(r.Incomplete),
		Unresolved:      newSprintCards(r.Unresolved),
		CompletedPoints: r.CompletedPoints, IncompletePoints: r.IncompletePoints,
		Carried: make([]sprintCarryPayload, 0, len(r.Carried)),
	}
	for _, c := range r.Carried {
		out.Carried = append(out.Carried, sprintCarryPayload{
			Ref: c.Ref, Action: string(c.Action), Sprint: c.Sprint,
			Status: string(c.Status), Error: c.Error,
		})
	}
	return out
}

// newSprintWrite renders the answer of a writing sprint call.
func newSprintWrite(result corevault.SprintResult) sprintWritePayload {
	out := sprintWritePayload{
		Sprint:  newSprintRow(result.Sprint.Sprint),
		DryRun:  result.DryRun,
		Written: make([]sprintWrittenFile, 0, len(result.Writes)),
	}
	for _, set := range result.Writes {
		for _, f := range set.Written {
			out.Written = append(out.Written, sprintWrittenFile{Repo: set.VaultID, Path: f.Path})
		}
	}
	if result.Report != nil {
		report := newSprintReport(*result.Report)
		out.Report = &report
	}
	if result.Board != nil {
		out.Board = &sprintBoardPayload{ID: result.Board.ID, Sprint: result.Board.Sprint}
	}
	return out
}

// -------------------------------------------------------------- the tree ---

// sprintFlags are the flags every sprint subcommand shares.
type sprintFlags struct {
	team   string
	asJSON bool
}

// newSprintCommand groups the cadence surface.
func newSprintCommand(flags *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sprint",
		Short: "List, start, close and transfer sprints",
		Long: `Drive the cadence of a team repository from the terminal.

A sprint file lives in the team repository and its items live in the project
repositories, so a sprint reports references a machine that cloned everything
can grade and references it cannot. Closing or transferring never guesses: it
reports what it could apply and what it refused, item by item.

` + "`--dry-run`" + ` computes the whole report and writes nothing, which is what a CI
job asking "is this sprint clean?" wants. Every subcommand takes --json, and in
that mode the human lines go to standard error.`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return usageError(cmd.Help())
		},
	}
	cmd.AddCommand(
		newSprintListCommand(flags),
		newSprintShowCommand(flags),
		newSprintStartCommand(flags),
		newSprintCloseCommand(flags),
		newSprintTransferCommand(flags),
	)
	return cmd
}

// teamFlag declares the --team flag every subcommand shares.
func teamFlag(cmd *cobra.Command, local *sprintFlags) {
	cmd.Flags().StringVar(&local.team, "team", "",
		"id of the registered team repository (default: the only one)")
	cmd.Flags().BoolVar(&local.asJSON, "json", false, "print machine-readable JSON")
}

// ------------------------------------------------------------------ list ---

// sprintListFlags mirrors the flags of the sprint listing.
type sprintListFlags struct {
	sprintFlags
	board    string
	statuses []string
}

// newSprintListCommand prints the sprints of a team, grouped by derived status.
func newSprintListCommand(flags *globalFlags) *cobra.Command {
	local := &sprintListFlags{}

	cmd := &cobra.Command{
		Use:     "list",
		Short:   "List the sprints of the team repository",
		Aliases: []string{"ls"},
		Long: `List the sprints of the team repository, grouped by the status derived from
their dates and today: draft, upcoming, current or completed.

The derived status is computed on every read and never written to the file, so
a sprint becomes current because the calendar says so and not because someone
remembered to edit it.`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runSprintList(cmd, flags, local)
		},
	}
	cmd.Flags().StringVar(&local.board, "board", "", "board id")
	cmd.Flags().StringArrayVar(&local.statuses, "status", nil,
		"derived status: draft, upcoming, current or completed (repeatable)")
	teamFlag(cmd, &local.sprintFlags)
	return cmd
}

// sprintHeaders are the columns of the sprint table.
var sprintHeaders = []string{"ID", "TITLE", "BOARD", "STATE", "DATES", "ITEMS", "DONE", "POINTS"}

// runSprintList renders the sprints in group order.
func runSprintList(cmd *cobra.Command, flags *globalFlags, local *sprintListFlags) error {
	params := corevault.SprintListParams{
		TeamScope: corevault.TeamScope{Team: strings.TrimSpace(local.team)},
		Board:     strings.TrimSpace(local.board),
	}
	for _, raw := range local.statuses {
		status, err := core.ParseSprintStatus(raw)
		if err != nil {
			return usagef("%v", err)
		}
		params.Status = append(params.Status, status)
	}
	space, err := openSpaceFor(flags)
	if err != nil {
		return err
	}
	result, err := dispatch[corevault.SprintListResult](cmd.Context(), space, "sprint.list", params)
	if err != nil {
		return err
	}

	payload := sprintListPayload{
		Sprints: make([]sprintRowPayload, 0, len(result.Sprints)),
		Counts:  map[string]int{},
		Total:   len(result.Sprints),
	}
	for _, status := range sprintStatusOrder {
		payload.Counts[string(status)] = 0
	}
	for _, status := range sprintStatusOrder {
		for _, s := range result.Sprints {
			if s.Status != status {
				continue
			}
			payload.Counts[string(status)]++
			payload.Sprints = append(payload.Sprints, newSprintRow(s))
		}
	}

	p := flags.printer(cmd, local.asJSON)
	if p.JSONMode() {
		return render(p.JSON(payload))
	}
	if len(payload.Sprints) == 0 {
		p.Printf("no sprint matches\n")
		return nil
	}
	for _, status := range sprintStatusOrder {
		rows := make([][]string, 0, payload.Counts[string(status)])
		for _, s := range payload.Sprints {
			if s.Status == string(status) {
				rows = append(rows, sprintRow(s))
			}
		}
		if len(rows) == 0 {
			continue
		}
		p.Printf("%s\n", strings.ToUpper(string(status)))
		if err := p.Table(sprintHeaders, rows); err != nil {
			return render(err)
		}
		p.Printf("\n")
	}
	p.Printf("%s\n", plural(payload.Total, "sprint", "sprints"))
	return nil
}

// sprintStatusOrder is the order the listing groups by: the way a cadence
// reads, from what is being planned to what is over.
var sprintStatusOrder = []core.SprintStatus{
	core.SprintStatusCurrent,
	core.SprintStatusUpcoming,
	core.SprintStatusDraft,
	core.SprintStatusCompleted,
}

// sprintRow renders one table row.
func sprintRow(s sprintRowPayload) []string {
	return []string{
		s.ID, s.Title, orDash(s.Board), s.State, sprintDates(s),
		itoa(s.Items), itoa(s.Done), points(s.DonePoints) + "/" + points(s.Points),
	}
}

// sprintDates renders the date range of a sprint, or a dash for a draft.
func sprintDates(s sprintRowPayload) string {
	switch {
	case s.Start == "" && s.End == "":
		return dash
	case s.End == "":
		return s.Start
	case s.Start == "":
		return s.End
	default:
		return s.Start + "→" + s.End
	}
}

// ------------------------------------------------------------------ show ---

// newSprintShowCommand prints one sprint and its scope.
func newSprintShowCommand(flags *globalFlags) *cobra.Command {
	local := &sprintFlags{}

	cmd := &cobra.Command{
		Use:   "show <id>",
		Short: "Show one sprint and its scope",
		Long: `Show one sprint: its header, the derived status, the metrics and every
reference in its scope.

A reference into a project this machine has not cloned still gets a line,
carrying the reason nothing could resolve it — the sprint keeps the reference
either way.`,
		Args: exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSprintShow(cmd, flags, local, args[0])
		},
	}
	teamFlag(cmd, local)
	return cmd
}

// sprintCardHeaders are the columns of the scope table.
var sprintCardHeaders = []string{"REF", "TITLE", "STATUS", "POINTS", "COMMITTED", "NOTE"}

// runSprintShow renders one sprint.
func runSprintShow(cmd *cobra.Command, flags *globalFlags, local *sprintFlags, id string) error {
	space, err := openSpaceFor(flags)
	if err != nil {
		return err
	}
	params := corevault.SprintParams{
		TeamScope: corevault.TeamScope{Team: strings.TrimSpace(local.team)},
		ID:        strings.TrimSpace(id),
	}
	view, err := dispatch[core.SprintView](cmd.Context(), space, "sprint.get", params)
	if err != nil {
		return err
	}
	payload := sprintShowPayload{
		Sprint: newSprintRow(view.Sprint),
		Cards:  newSprintCards(view.Cards),
	}
	for _, d := range view.Diagnostics {
		payload.Diagnostics = append(payload.Diagnostics, d.Message)
	}

	p := flags.printer(cmd, local.asJSON)
	if p.JSONMode() {
		return render(p.JSON(payload))
	}
	printSprintHeader(p, payload.Sprint)
	rows := make([][]string, 0, len(payload.Cards))
	for _, c := range payload.Cards {
		rows = append(rows, []string{
			c.Ref, orDash(c.Title), orDash(c.Status), points(c.Points),
			yesNo(c.Committed), orDash(c.Reason),
		})
	}
	if len(rows) > 0 {
		if err := p.Table(sprintCardHeaders, rows); err != nil {
			return render(err)
		}
	}
	for _, d := range payload.Diagnostics {
		p.Warnf("warning: %s\n", d)
	}
	return nil
}

// printSprintHeader prints the human header of one sprint.
func printSprintHeader(p *output.Printer, s sprintRowPayload) {
	p.Printf("%s  %s\n", s.ID, s.Title)
	p.Printf("board %s, state %s, status %s, %s\n",
		orDash(s.Board), s.State, s.Status, sprintDates(s))
	if s.Goal != "" {
		p.Printf("goal: %s\n", s.Goal)
	}
	p.Printf("%s, %d done, %s of %s points, %d unresolved\n",
		plural(s.Items, "item", "items"), s.Done,
		points(s.DonePoints), points(s.Points), s.Unresolved)
}

// ----------------------------------------------------------------- start ---

// sprintStartFlags mirrors the flags of starting a sprint.
type sprintStartFlags struct {
	sprintFlags
	force bool
}

// newSprintStartCommand makes a sprint active.
func newSprintStartCommand(flags *globalFlags) *cobra.Command {
	local := &sprintStartFlags{}

	cmd := &cobra.Command{
		Use:   "start <id>",
		Short: "Start a sprint",
		Long: `Start a sprint: it becomes active, its scope is copied into the commitment so
that what was promised stays legible next to what was pulled in later, and the
board it belongs to is pointed at it.

Starting a second sprint on the same board is refused once; --force does it
anyway, so that two active sprints are never an accident.`,
		Args: exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSprintStart(cmd, flags, local, args[0])
		},
	}
	cmd.Flags().BoolVar(&local.force, "force", false, "start a sprint on a board that already runs one")
	teamFlag(cmd, &local.sprintFlags)
	return cmd
}

// runSprintStart starts a sprint and reports what it wrote.
func runSprintStart(cmd *cobra.Command, flags *globalFlags, local *sprintStartFlags, id string) error {
	space, err := openSpaceFor(flags)
	if err != nil {
		return err
	}
	params := corevault.SprintStartParams{
		TeamScope: corevault.TeamScope{Team: strings.TrimSpace(local.team)},
		ID:        strings.TrimSpace(id),
		Force:     local.force,
	}
	result, err := dispatch[corevault.SprintResult](cmd.Context(), space, "sprint.start", params)
	if err != nil {
		return err
	}
	payload := newSprintWrite(result)

	p := flags.printer(cmd, local.asJSON)
	if p.JSONMode() {
		return render(p.JSON(payload))
	}
	p.Printf("started %s (%s), %s committed\n",
		payload.Sprint.ID, sprintDates(payload.Sprint),
		plural(payload.Sprint.Items, "item", "items"))
	if payload.Board != nil {
		p.Printf("board %s now runs %s\n", payload.Board.ID, payload.Board.Sprint)
	}
	printWritten(p, payload.Written)
	return nil
}

// ----------------------------------------------------------------- close ---

// sprintCloseFlags mirrors the flags of closing a sprint.
type sprintCloseFlags struct {
	sprintFlags
	transfer string
	target   string
	dryRun   bool
}

// newSprintCloseCommand closes a sprint and reports what it found.
func newSprintCloseCommand(flags *globalFlags) *cobra.Command {
	local := &sprintCloseFlags{}

	cmd := &cobra.Command{
		Use:   "close <id>",
		Short: "Close a sprint and decide what happens to the unfinished work",
		Long: `Close a sprint: freeze its progress into the file, report what was finished and
what was not, and apply one decision to every unfinished reference.

--transfer next carries the unfinished work into another sprint of the same
board, --transfer backlog sends it back to its own project's backlog, and
--transfer none — the default — moves nothing and only reports. --target names
the sprint "next" carries into; without it the earliest planned sprint of the
board is used.

--dry-run computes the whole report and writes nothing, not even the snapshot.`,
		Args: exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSprintClose(cmd, flags, local, args[0])
		},
	}
	f := cmd.Flags()
	f.StringVar(&local.transfer, "transfer", corevault.TransferNone,
		"what happens to the unfinished work: next, backlog or none")
	f.StringVar(&local.target, "target", "", "sprint id `next` carries into")
	f.BoolVar(&local.dryRun, "dry-run", false, "report what would happen without writing anything")
	teamFlag(cmd, &local.sprintFlags)
	return cmd
}

// runSprintClose closes a sprint.
func runSprintClose(cmd *cobra.Command, flags *globalFlags, local *sprintCloseFlags, id string) error {
	mode, err := parseTransferMode(local.transfer)
	if err != nil {
		return err
	}
	space, err := openSpaceFor(flags)
	if err != nil {
		return err
	}
	params := corevault.SprintCloseParams{
		TeamScope: corevault.TeamScope{Team: strings.TrimSpace(local.team)},
		ID:        strings.TrimSpace(id),
		Transfer:  &corevault.SprintTransfer{Mode: mode, Target: strings.TrimSpace(local.target)},
		DryRun:    local.dryRun,
	}
	result, err := dispatch[corevault.SprintResult](cmd.Context(), space, "sprint.close", params)
	if err != nil {
		return err
	}
	payload := newSprintWrite(result)

	p := flags.printer(cmd, local.asJSON)
	if p.JSONMode() {
		if err := render(p.JSON(payload)); err != nil {
			return err
		}
	} else {
		p.Printf("%s %s\n", closeVerb(payload.DryRun), payload.Sprint.ID)
		printSprintReport(p, payload)
		printWritten(p, payload.Written)
	}
	return carryOutcome(p, payload)
}

// closeVerb names what a close did, so that a preview never reads as a
// commitment.
func closeVerb(dryRun bool) string {
	if dryRun {
		return "would close"
	}
	return "closed"
}

// -------------------------------------------------------------- transfer ---

// sprintTransferFlags mirrors the flags of a standalone transfer.
type sprintTransferFlags struct {
	sprintFlags
	to     string
	dryRun bool
}

// newSprintTransferCommand moves the unfinished work of a sprint elsewhere.
func newSprintTransferCommand(flags *globalFlags) *cobra.Command {
	local := &sprintTransferFlags{}

	cmd := &cobra.Command{
		Use:   "transfer <id> --to <SPRINT-ID>",
		Short: "Move the unfinished work of a sprint into another sprint",
		Long: `Move every unfinished reference of a sprint into another sprint of the same
board, without closing either of them.

The source sprint is not edited: its scope keeps every reference it ever held,
and its commitment — the record of what was promised — is never touched.
--dry-run computes the report and writes nothing.`,
		Args: exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(local.to) == "" {
				return usagef("--to is required: name the sprint the work moves into")
			}
			return runSprintTransfer(cmd, flags, local, args[0])
		},
	}
	f := cmd.Flags()
	f.StringVar(&local.to, "to", "", "sprint id the unfinished work moves into")
	f.BoolVar(&local.dryRun, "dry-run", false, "report what would happen without writing anything")
	teamFlag(cmd, &local.sprintFlags)
	return cmd
}

// runSprintTransfer moves the unfinished references of a sprint.
func runSprintTransfer(cmd *cobra.Command, flags *globalFlags, local *sprintTransferFlags, id string) error {
	space, err := openSpaceFor(flags)
	if err != nil {
		return err
	}
	params := corevault.SprintTransferParams{
		TeamScope: corevault.TeamScope{Team: strings.TrimSpace(local.team)},
		ID:        strings.TrimSpace(id),
		Mode:      corevault.TransferNext,
		Target:    strings.TrimSpace(local.to),
		DryRun:    local.dryRun,
	}
	result, err := dispatch[corevault.SprintResult](cmd.Context(), space, "sprint.transfer", params)
	if err != nil {
		return err
	}
	payload := newSprintWrite(result)

	p := flags.printer(cmd, local.asJSON)
	if p.JSONMode() {
		if err := render(p.JSON(payload)); err != nil {
			return err
		}
	} else {
		verb := "transferred"
		if payload.DryRun {
			verb = "would transfer"
		}
		p.Printf("%s the unfinished work of %s into %s\n",
			verb, payload.Sprint.ID, strings.TrimSpace(local.to))
		printSprintReport(p, payload)
		printWritten(p, payload.Written)
	}
	return carryOutcome(p, payload)
}

// --------------------------------------------------------------- shared ----

// parseTransferMode validates the bulk transfer mode of a close.
func parseTransferMode(raw string) (string, error) {
	switch mode := strings.ToLower(strings.TrimSpace(raw)); mode {
	case "", corevault.TransferNone:
		return corevault.TransferNone, nil
	case corevault.TransferNext, corevault.TransferBacklog:
		return mode, nil
	default:
		return "", usagef("unknown transfer mode %q: use next, backlog or none", raw)
	}
}

// printSprintReport prints the human form of a close or transfer report.
func printSprintReport(p *output.Printer, payload sprintWritePayload) {
	report := payload.Report
	if report == nil {
		return
	}
	p.Printf("%d completed (%s points), %d incomplete (%s points), %d unresolved\n",
		len(report.Completed), points(report.CompletedPoints),
		len(report.Incomplete), points(report.IncompletePoints),
		len(report.Unresolved))
	for _, c := range report.Carried {
		if c.Error != "" {
			// A per-item refusal is printed on its own line: the rest of the
			// operation still went through, and a reader has to see which
			// reference did not move and why.
			p.Printf("refused %s: %s\n", c.Ref, c.Error)
			continue
		}
		switch {
		case c.Sprint != "":
			p.Printf("%s %s → %s\n", c.Action, c.Ref, c.Sprint)
		case c.Status != "":
			p.Printf("%s %s → %s\n", c.Action, c.Ref, c.Status)
		default:
			p.Printf("%s %s\n", c.Action, c.Ref)
		}
	}
}

// carryOutcome turns the per-item refusals into an exit code. A refusal is not
// a failure on its own — the operation reports it and carries on — so the
// process only fails when there were decisions to apply and none of them could
// be applied.
func carryOutcome(p *output.Printer, payload sprintWritePayload) error {
	if payload.Report == nil || len(payload.Report.Carried) == 0 {
		return nil
	}
	refused := 0
	for _, c := range payload.Report.Carried {
		if c.Error != "" {
			refused++
		}
	}
	if refused == 0 {
		return nil
	}
	if refused < len(payload.Report.Carried) {
		p.Warnf("%d of %d decisions were refused\n", refused, len(payload.Report.Carried))
		return nil
	}
	return failf(exitFailure, "none of the %d decisions could be applied", refused)
}

// printWritten lists the files an operation saved.
func printWritten(p *output.Printer, written []sprintWrittenFile) {
	if len(written) == 0 {
		return
	}
	for _, f := range written {
		p.Printf("wrote %s\n", displayPath(f.Repo+"/"+f.Path))
	}
}

// yesNo renders a boolean cell.
func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// itoa renders a count as a table cell.
func itoa(n int) string { return strconv.Itoa(n) }

// points renders a story-point total without a trailing ".0", because a
// fibonacci scale is read as whole numbers and half points are rare.
func points(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}
