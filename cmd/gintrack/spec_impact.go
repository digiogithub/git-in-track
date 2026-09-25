package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/digiogithub/git-in-track/internal/core"
)

// specImpactFlags mirrors the flags of docs/07 section 4.20.
type specImpactFlags struct {
	since   string
	head    string
	story   string
	project string
	title   string
	budget  int
	cursor  string
	tiers   []int
	format  string
	pretty  bool
	failOn  []string
	asJSON  bool
}

// specImpactOffender is one hit that tripped --fail-on.
type specImpactOffender struct {
	Ref     core.RequirementRef `json:"ref"`
	Title   string              `json:"title"`
	Tier    int                 `json:"tier"`
	Status  core.CoverageStatus `json:"status,omitempty"`
	Suspect bool                `json:"suspect,omitempty"`
}

// specImpactPayload is what `gintrack spec impact --json` prints.
type specImpactPayload struct {
	Report    core.ImpactReport    `json:"report"`
	FailOn    []string             `json:"failOn,omitempty"`
	Offending []specImpactOffender `json:"offending,omitempty"`
}

func newSpecImpactCommand(flags *globalFlags) *cobra.Command {
	local := &specImpactFlags{}
	cmd := &cobra.Command{
		Use:   "impact --since <ref>",
		Short: "Report the requirements a diff affects",
		Long: `Resolve the requirements a diff affects (docs/03 section 21.11), in three
tiers: 1 the direct trace (Implements:/Verifies: markers, trace: entries, and
the Spec Delta and implements/modifies links of --story), 2 the transitive
callers and 3 semantic candidates. Tiers 2 and 3 read Pando; the command line
has no Pando client, so they report unavailable and tier 1 still answers.

The diff runs from --since to --head; without --head it ends at the working
tree. The report is ranked failing, then suspect, then by tier, and cut at
--budget tokens; walk the rest with --cursor. The default output is the terse
text report; --format json (or --json) prints the report's structured form as
one line of compact JSON, the form the report's "tokens" estimate measures
(unlike the indented JSON of other commands); --pretty indents it.

--fail-on lists coverage states that fail the run: when any hit of the whole
result — not just the page shown — is in one of them, the report is still
printed, the offending requirements are listed on stderr and the command exits
7. "suspect" also matches a passing requirement whose traced code the diff
changes. Tier-3 semantic candidates never trip it: they are neighbors, not
traces.`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runSpecImpact(cmd, flags, local)
		},
	}
	f := cmd.Flags()
	f.StringVar(&local.since, "since", "", "revision the diff starts from: a branch, a SHA or HEAD (required)")
	f.StringVar(&local.head, "head", "", "revision the diff ends at (default: the working tree)")
	f.StringVar(&local.story, "story", "", "story or task the diff is for: its Spec Delta and spec links are direct hits")
	f.StringVar(&local.project, "project", "", "project key whose repository holds the diff (needed with several projects)")
	f.StringVar(&local.title, "title", "", "extra text for the semantic tier")
	f.IntVar(&local.budget, "budget", 0, "token budget of the page, 1 to 20000 (default 1500)")
	f.StringVar(&local.cursor, "cursor", "", "nextCursor of the previous page, with the query unchanged")
	f.IntSliceVar(&local.tiers, "tiers", nil, "tiers to run, from 1, 2 and 3 (default all)")
	f.StringVar(&local.format, "format", "", "report form: text (default) or json")
	f.StringSliceVar(&local.failOn, "fail-on", nil, "exit 7 when a hit is in one of these states: untested, passing, failing, suspect")
	f.BoolVar(&local.asJSON, "json", false, "print machine-readable JSON (compact; see --pretty)")
	f.BoolVar(&local.pretty, "pretty", false, "indent the JSON output; the report's tokens estimate measures the compact form")
	return cmd
}

// checkSpecImpactFlags validates what the command line can decide on its own,
// so a typo is a usage error (2) rather than a failed query.
func checkSpecImpactFlags(local *specImpactFlags) ([]core.CoverageStatus, error) {
	if strings.TrimSpace(local.since) == "" {
		return nil, usagef("--since is required: the revision the diff starts from, e.g. origin/main")
	}
	switch local.format {
	case "", string(core.ImpactReportText), string(core.ImpactReportJSON):
	default:
		return nil, usagef("unknown --format %q: use text or json", local.format)
	}
	if local.budget < 0 || local.budget > core.MaxImpactBudget {
		return nil, usagef("--budget %d is out of range: use 1 to %d", local.budget, core.MaxImpactBudget)
	}
	for _, t := range local.tiers {
		if t < core.ImpactTierDirect || t > core.ImpactTierSemantic {
			return nil, usagef("unknown tier %d in --tiers: use 1, 2 or 3", t)
		}
	}
	var failOn []core.CoverageStatus
	for _, s := range local.failOn {
		st := core.CoverageStatus(strings.TrimSpace(s))
		if !st.Valid() {
			return nil, usagef("unknown state %q in --fail-on: use untested, passing, failing or suspect", s)
		}
		failOn = append(failOn, st)
	}
	return failOn, nil
}

func runSpecImpact(cmd *cobra.Command, flags *globalFlags, local *specImpactFlags) error {
	failOn, err := checkSpecImpactFlags(local)
	if err != nil {
		return err
	}
	s, err := openSpecSpace(cmd, flags, specSpaceOptions{seams: true})
	if err != nil {
		return err
	}
	defer s.close()
	if err := s.needsProject(local.project, local.story); err != nil {
		return err
	}
	head := strings.TrimSpace(local.head)
	if strings.EqualFold(head, "worktree") {
		head = ""
	}
	story := strings.TrimSpace(local.story)
	// One resolution serves both the page shown and the gate, which must see
	// every hit and not only the ones the budget let through.
	got, err := dispatch[struct {
		Impact core.ImpactResult `json:"impact"`
	}](cmd.Context(), s.space, "impact.query", map[string]any{
		"base": strings.TrimSpace(local.since), "head": head,
		"story": story, "title": local.title, "tiers": local.tiers,
		"project": local.project,
		// The story routes the call to the repository that holds it.
		"id": story,
	})
	if err != nil {
		return err
	}

	jsonOut := local.asJSON || local.format == string(core.ImpactReportJSON)
	format := core.ImpactReportFormat(local.format)
	if format == "" {
		format = core.ImpactReportText
		if jsonOut {
			format = core.ImpactReportJSON
		}
	}
	report, err := core.RenderImpactReport(got.Impact, core.ImpactReportOptions{
		Budget: local.budget, Cursor: local.cursor, Format: format,
	})
	if errors.Is(err, core.ErrInvalidCursor) {
		return usageError(err)
	}
	if err != nil {
		return fmt.Errorf("render the impact report: %w", err)
	}

	payload := specImpactPayload{Report: report, Offending: impactOffenders(got.Impact.Hits, failOn)}
	for _, st := range failOn {
		payload.FailOn = append(payload.FailOn, string(st))
	}
	p := flags.printer(cmd, jsonOut)
	// The JSON is compact by default so that what is printed is what the
	// report's tokens estimate measured (GIT-US-0160).
	p.SetCompact(!local.pretty)
	if p.JSONMode() {
		if err := render(p.JSON(payload)); err != nil {
			return err
		}
	} else if report.Text != "" {
		p.Printf("%s\n", strings.TrimRight(report.Text, "\n"))
	}
	if len(payload.Offending) == 0 {
		return nil
	}
	for _, o := range payload.Offending {
		state := string(o.Status)
		if o.Suspect && o.Status != core.CoverageSuspect {
			state += ", suspect"
		}
		p.Warnf("%s  %s  %s\n", o.Ref, state, o.Title)
	}
	return failf(exitGate, "%s in a --fail-on state (%s)",
		plural(len(payload.Offending), "requirement is", "requirements are"), strings.Join(payload.FailOn, ","))
}

// impactOffenders returns the hits in one of the --fail-on states, in the
// report's rank order. "suspect" matches the suspect flag too, which the
// impact query also raises on a passing requirement whose traced code the
// diff changes. A tier-3 candidate is a semantic neighbor, not a trace, and
// never trips the gate.
func impactOffenders(hits []core.ImpactHit, failOn []core.CoverageStatus) []specImpactOffender {
	if len(failOn) == 0 {
		return nil
	}
	want := map[core.CoverageStatus]bool{}
	for _, st := range failOn {
		want[st] = true
	}
	var out []specImpactOffender
	for _, h := range core.RankImpactHits(hits) {
		if h.Candidate {
			continue
		}
		if want[h.Status] || (h.Suspect && want[core.CoverageSuspect]) {
			out = append(out, specImpactOffender{Ref: h.Ref, Title: h.Title, Tier: h.Tier, Status: h.Status, Suspect: h.Suspect})
		}
	}
	return out
}
