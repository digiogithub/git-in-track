package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/digiogithub/git-in-track/internal/core"
)

// This file is `gintrack spec coverage` and `gintrack spec trace` (docs/07
// section 4.20): shims over the vault methods "coverage.list" and
// "trace.requirement", the same ones spec_coverage and trace_requirement call
// over MCP.

// specCoverageFlags mirrors the flags of docs/07 section 4.20.
type specCoverageFlags struct {
	spec    string
	project string
	status  []string
	asJSON  bool
}

// specCoveragePayload is what `gintrack spec coverage --json` prints.
type specCoveragePayload struct {
	Coverage []core.CoverageRow `json:"coverage"`
	Total    int                `json:"total"`
}

func newSpecCoverageCommand(flags *globalFlags) *cobra.Command {
	local := &specCoverageFlags{}
	cmd := &cobra.Command{
		Use:   "coverage",
		Short: "Show the coverage state of every requirement",
		Long: `Print one row per requirement with its coverage state (docs/03 section 21.6):
untested, passing, failing or suspect, the reason codes behind it and how many
of its linked tests passed in the results "gintrack spec ingest" recorded.
Nothing is written.`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runSpecCoverage(cmd, flags, local)
		},
	}
	f := cmd.Flags()
	f.StringVar(&local.spec, "spec", "", "only the requirements of this spec")
	f.StringVar(&local.project, "project", "", "only the requirements of this project")
	f.StringSliceVar(&local.status, "status", nil, "only these states: untested, passing, failing, suspect")
	f.BoolVar(&local.asJSON, "json", false, "print machine-readable JSON")
	return cmd
}

func runSpecCoverage(cmd *cobra.Command, flags *globalFlags, local *specCoverageFlags) error {
	for _, s := range local.status {
		if !core.CoverageStatus(strings.TrimSpace(s)).Valid() {
			return usagef("unknown state %q in --status: use untested, passing, failing or suspect", s)
		}
	}
	if local.spec != "" && !isSpecID(local.spec) {
		return usagef("%q is not a spec id: want <KEY>-SP-<NNNN>", local.spec)
	}
	s, err := openSpecSpace(cmd, flags, specSpaceOptions{seams: true})
	if err != nil {
		return err
	}
	params := map[string]any{"spec": local.spec, "project": local.project, "status": local.status}
	// A spec or a project names one repository; otherwise every project
	// repository answers for its own requirements.
	var calls []map[string]any
	if local.spec != "" || local.project != "" {
		calls = append(calls, params)
	} else {
		for _, m := range s.projectMounts() {
			call := map[string]any{"vaultId": m.ID}
			for k, v := range params {
				call[k] = v
			}
			calls = append(calls, call)
		}
	}
	payload := specCoveragePayload{Coverage: []core.CoverageRow{}}
	for _, call := range calls {
		got, err := dispatch[specCoveragePayload](cmd.Context(), s.space, "coverage.list", call)
		if err != nil {
			return err
		}
		payload.Coverage = append(payload.Coverage, got.Coverage...)
	}
	payload.Total = len(payload.Coverage)

	p := flags.printer(cmd, local.asJSON)
	if p.JSONMode() {
		return render(p.JSON(payload))
	}
	rows := make([][]string, 0, len(payload.Coverage))
	for _, r := range payload.Coverage {
		passed := 0
		for _, t := range r.Tests {
			if t.Result == "pass" {
				passed++
			}
		}
		tests := "-"
		if len(r.Tests) > 0 {
			tests = fmt.Sprintf("%d/%d", passed, len(r.Tests))
		}
		rows = append(rows, []string{r.Ref.String(), string(r.Status), tests, strings.Join(r.Reasons, ",")})
	}
	if err := render(p.Table([]string{"REF", "STATUS", "TESTS", "REASONS"}, rows)); err != nil {
		return err
	}
	p.Printf("%s\n", plural(payload.Total, "requirement", "requirements"))
	return nil
}

// specTraceFlags mirrors the flags of docs/07 section 4.20.
type specTraceFlags struct {
	asJSON bool
}

func newSpecTraceCommand(flags *globalFlags) *cobra.Command {
	local := &specTraceFlags{}
	cmd := &cobra.Command{
		Use:   "trace <ref>",
		Short: "Show the code, tests and work traced to one requirement",
		Long: `Print the trace of one requirement (docs/03 section 21.7): the code that
realizes it and the tests that verify it as path#symbol, each with its origin —
marker (an Implements:/Verifies: comment) or trace (a trace: entry of the spec)
— and the marker lines; the stories and tasks that implement or modify it; and
the trace: entries that no longer resolve.`,
		Args: exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSpecTrace(cmd, flags, local, args[0])
		},
	}
	cmd.Flags().BoolVar(&local.asJSON, "json", false, "print machine-readable JSON")
	return cmd
}

func runSpecTrace(cmd *cobra.Command, flags *globalFlags, local *specTraceFlags, raw string) error {
	ref := strings.TrimSpace(raw)
	if !validRequirementArg(ref) {
		return usagef("%q is not a requirement ref: want <KEY>-SP-<NNNN>.R<n>", raw)
	}
	s, err := openSpecSpace(cmd, flags, specSpaceOptions{seams: true})
	if err != nil {
		return err
	}
	got, err := dispatch[struct {
		Trace core.TracedRequirement `json:"trace"`
	}](cmd.Context(), s.space, "trace.requirement", map[string]any{"ref": ref})
	if err != nil {
		return err
	}
	tr := got.Trace

	p := flags.printer(cmd, local.asJSON)
	if p.JSONMode() {
		return render(p.JSON(tr))
	}
	p.Printf("%s\n", tr.Ref)
	printEdges(p.Printf, "code", tr.Code)
	printEdges(p.Printf, "tests", tr.Tests)
	if len(tr.Work) > 0 {
		p.Printf("work:\n")
		for _, w := range tr.Work {
			whole := ""
			if w.WholeSpec {
				whole = " (whole spec)"
			}
			p.Printf("  %s  %s%s\n", w.ID, w.Kind, whole)
		}
	}
	if len(tr.Broken) > 0 {
		p.Printf("broken:\n")
		for _, b := range tr.Broken {
			p.Printf("  %s  %s  %s\n", b.Field, b.Entry, b.Code)
		}
	}
	return nil
}

// printEdges prints one section of a trace: path#symbol, origin and lines.
func printEdges(printf func(string, ...any), title string, edges []core.TraceEdge) {
	if len(edges) == 0 {
		printf("%s: none\n", title)
		return
	}
	printf("%s:\n", title)
	for _, e := range edges {
		origin := make([]string, 0, len(e.Sources))
		for _, src := range e.Sources {
			origin = append(origin, string(src))
		}
		line := fmt.Sprintf("  %s  %s", e.TraceRef(), strings.Join(origin, ","))
		if len(e.Lines) > 0 {
			nums := make([]string, 0, len(e.Lines))
			for _, n := range e.Lines {
				nums = append(nums, strconv.Itoa(n))
			}
			line += "  lines " + strings.Join(nums, ",")
		}
		printf("%s\n", line)
	}
}
