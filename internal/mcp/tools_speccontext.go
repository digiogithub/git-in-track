package mcp

import (
	"context"
	"strings"

	"github.com/digiogithub/git-in-track/internal/core"
)

// The two spec reads an agent makes before it changes code (ADR-037, docs/03
// section 21.11, GIT-US-0123): spec_context answers "what does the story I
// picked require?" — the requirements it implements or modifies, what each
// says, its scenarios and whether its tests pass, and the knowledge-base pages
// around them, within a token budget — and spec_coverage answers "which
// requirements are untested, failing or suspect?". spec_context is a shim over
// the vault method "spec.context", which renders through the same token
// estimator and cursor as the impact report; spec_coverage pages the rows of
// "coverage.list" with this package's own cursor.

// -------------------------------------------------------------- wire shape --

// ContextScenario is one scenario of a requirement in a spec context.
type ContextScenario struct {
	Name  string   `json:"name"`
	Steps []string `json:"steps,omitempty" jsonschema:"The scenario's steps, only when the page's detail is steps"`
}

// ContextRequirement is one requirement of a spec context.
type ContextRequirement struct {
	Ref       string            `json:"ref" jsonschema:"Requirement ref; for an ADDED block not yet numbered, the spec it will join"`
	Title     string            `json:"title"`
	Via       []string          `json:"via" jsonschema:"implements, modifies (declared links), delta-added, delta-modified, delta-removed or delta-superseded (the item's Spec Delta)"`
	WholeSpec bool              `json:"wholeSpec,omitempty" jsonschema:"Reached through a link to its whole spec"`
	Proposed  bool              `json:"proposed,omitempty" jsonschema:"Statement and scenarios are the Spec Delta's proposal, not the spec's current text"`
	Statement string            `json:"statement,omitempty" jsonschema:"The statement on one line, clipped"`
	Scenarios []ContextScenario `json:"scenarios,omitempty"`
	Status    string            `json:"status,omitempty" jsonschema:"Coverage state: untested, passing, failing or suspect; absent when coverage is unavailable"`
	Reasons   []string          `json:"reasons,omitempty" jsonschema:"Short coverage reason codes"`
}

// ContextPage is a knowledge-base page related to the item or its specs.
type ContextPage struct {
	Path  string `json:"path" jsonschema:"Vault-relative path; read it with get_kb_page"`
	Title string `json:"title,omitempty"`
}

// SpecContext is one page of the token-budgeted spec context of a story or
// task.
type SpecContext struct {
	Item         string               `json:"item"`
	Title        string               `json:"title"`
	Coverage     string               `json:"coverage,omitempty" jsonschema:"ok, or unavailable when this session has no coverage backend (browser-only mode)"`
	Detail       string               `json:"detail" jsonschema:"steps (scenario steps included) or names (scenario names only)"`
	Requirements []ContextRequirement `json:"requirements,omitempty" jsonschema:"The requirements of this page (json form)"`
	Pages        []ContextPage        `json:"pages,omitempty" jsonschema:"Related knowledge-base pages, on the first page only (json form)"`
	MorePages    int                  `json:"morePages,omitempty" jsonschema:"Related pages left out past the first ten"`
	Text         string               `json:"text,omitempty" jsonschema:"The page as text lines (text form)"`
	Total        int                  `json:"total"`
	Offset       int                  `json:"offset,omitempty"`
	Truncated    int                  `json:"truncated,omitempty" jsonschema:"Requirements after this page"`
	NextCursor   string               `json:"nextCursor,omitempty" jsonschema:"Pass back as cursor with the same story"`
	Budget       int                  `json:"budget"`
	Tokens       int                  `json:"tokens" jsonschema:"Estimated tokens of this page"`
}

// CoverageTest is one linked test of a requirement with its latest result.
type CoverageTest struct {
	Test   string `json:"test" jsonschema:"Repository-relative path#symbol"`
	Result string `json:"result" jsonschema:"pass, fail, skip, or missing when no ingested result matched it"`
}

// CoverageRow is the coverage of one requirement.
type CoverageRow struct {
	Ref     string         `json:"ref"`
	Status  string         `json:"status" jsonschema:"untested, passing, failing or suspect"`
	Reasons []string       `json:"reasons,omitempty" jsonschema:"Short reason codes, for example no-tests, failed, text, code:<path#symbol>"`
	Tests   []CoverageTest `json:"tests,omitempty"`
}

// CoverageCounts counts the rows of the whole filter by state.
type CoverageCounts struct {
	Untested int `json:"untested,omitempty"`
	Passing  int `json:"passing,omitempty"`
	Failing  int `json:"failing,omitempty"`
	Suspect  int `json:"suspect,omitempty"`
}

// CoveragePage is one page of coverage rows.
type CoveragePage struct {
	Coverage   []CoverageRow  `json:"coverage"`
	Total      int            `json:"total" jsonschema:"Rows the filter matches"`
	Counts     CoverageCounts `json:"counts" jsonschema:"Rows of the whole filter by state"`
	NextCursor string         `json:"nextCursor,omitempty" jsonschema:"Pass back as cursor to fetch the next page"`
}

// ------------------------------------------------------------------ input ---

// SpecContextInput names the story or task, and the page of its context.
type SpecContextInput struct {
	Story  string `json:"story" jsonschema:"Story or task id, for example ACME-US-0042"`
	Budget int    `json:"budget,omitempty" jsonschema:"Token budget of the page, 1 to 20000; default 1500"`
	Cursor string `json:"cursor,omitempty" jsonschema:"nextCursor from the previous page, with the same story"`
	Format string `json:"format,omitempty" jsonschema:"json (default) or text (cheaper)"`
}

// SpecCoverageInput filters the coverage rows.
type SpecCoverageInput struct {
	Project string   `json:"project,omitempty" jsonschema:"Project key; needed when the workspace holds more than one and no spec is named"`
	Spec    string   `json:"spec,omitempty" jsonschema:"Spec id, for example ACME-SP-0003"`
	Status  []string `json:"status,omitempty" jsonschema:"Coverage states to keep: untested, passing, failing, suspect"`
	Fields  []string `json:"fields,omitempty" jsonschema:"Fields to project: reasons, tests. ref and status are always returned; default both"`
	Limit   int      `json:"limit,omitempty" jsonschema:"Page size, 1 to 100; default 20"`
	Cursor  string   `json:"cursor,omitempty" jsonschema:"nextCursor from the previous page, with the filter unchanged"`
}

// coverageStates are the four computed states, for the errors that teach.
var coverageStates = []string{"untested", "passing", "failing", "suspect"}

// ---------------------------------------------------------------- registry --

// registerSpecContextTools declares the two spec reads of the agent loop.
func registerSpecContextTools(s *Server) {
	register(s, toolDef{
		Name:  "spec_context",
		Title: "Requirements of a story",
		Description: "Return what a story or task requires, within a token budget (default 1500): the " +
			"requirements it implements or modifies — its declared links, a link to a whole spec, and its " +
			"unapplied Spec Delta, ADDED blocks not yet numbered included — each with a one-line statement, " +
			"its scenarios, its coverage status and reasons, plus the knowledge-base pages the story and " +
			"those specs link. Call it first when you pick up a story. When the budget is tight the page " +
			"drops scenario steps first (detail: names), then cuts the requirement list: walk nextCursor " +
			"with the same story. coverage is unavailable when this session cannot read test results; " +
			"the rest still answers. format text is cheaper than json.",
		Untrusted: true,
	}, specContext)

	register(s, toolDef{
		Name:  "spec_coverage",
		Title: "Requirement coverage",
		Description: "List one compact row per requirement: its coverage state (untested, passing, failing " +
			"or suspect), short reason codes and its linked tests with their latest ingested result, " +
			"filtered by project, spec and status, with counts per state over the whole filter. Rows are " +
			"paginated: walk nextCursor with the filter unchanged. A session that cannot read test results " +
			"or git history answers unavailable.",
		Untrusted: true,
	}, specCoverage)
}

// ---------------------------------------------------------------- handlers --

// specContext answers the spec context of a story or task.
func specContext(ctx context.Context, s *Server, in SpecContextInput) (SpecContext, error) {
	story := strings.TrimSpace(in.Story)
	if story == "" {
		return SpecContext{}, invalidField("story", "spec_context needs the story or task you picked", "ACME-US-0042")
	}
	format := strings.ToLower(strings.TrimSpace(in.Format))
	if format != "" && format != string(core.ImpactReportJSON) && format != string(core.ImpactReportText) {
		return SpecContext{}, invalidField("format", "unknown context format "+in.Format, []string{"json", "text"})
	}
	got, err := dispatch[struct {
		Report SpecContext `json:"report"`
	}](ctx, s, "spec.context", map[string]any{
		// The id routes the call to the repository that holds the story.
		"id":     story,
		"budget": in.Budget,
		"cursor": in.Cursor,
		"format": format,
	})
	if err != nil {
		return SpecContext{}, err
	}
	return got.Report, nil
}

// specCoverage answers a filtered, paginated page of coverage rows.
func specCoverage(ctx context.Context, s *Server, in SpecCoverageInput) (CoveragePage, error) {
	status := make([]string, 0, len(in.Status))
	for _, st := range in.Status {
		st = strings.ToLower(strings.TrimSpace(st))
		if !core.CoverageStatus(st).Valid() {
			return CoveragePage{}, invalidField("status", "unknown coverage state "+st, coverageStates)
		}
		status = append(status, st)
	}
	limit := boundedLimit(in.Limit)
	filter := core.Fingerprint("spec_coverage", in.Project, in.Spec, status)
	offset, err := decodeCursor(in.Cursor, filter)
	if err != nil {
		return CoveragePage{}, err
	}
	got, err := dispatch[struct {
		Coverage []core.CoverageRow `json:"coverage"`
	}](ctx, s, "coverage.list", map[string]any{
		"project": in.Project,
		"spec":    strings.TrimSpace(in.Spec),
		"status":  status,
	})
	if err != nil {
		return CoveragePage{}, withFallback(err,
			"list the requirements with list_requirements; coverage needs a session that reads test results "+
				"and git history (`gintrack mcp` or the companion, after `gintrack spec ingest`)")
	}
	out := CoveragePage{Coverage: []CoverageRow{}, Total: len(got.Coverage)}
	for _, r := range got.Coverage {
		switch r.Status {
		case core.CoverageUntested:
			out.Counts.Untested++
		case core.CoveragePassing:
			out.Counts.Passing++
		case core.CoverageFailing:
			out.Counts.Failing++
		case core.CoverageSuspect:
			out.Counts.Suspect++
		}
	}
	wantReasons, wantTests := len(in.Fields) == 0, len(in.Fields) == 0
	if len(in.Fields) > 0 {
		wantReasons, wantTests = includes(in.Fields, "reasons"), includes(in.Fields, "tests")
	}
	page, next := slice(got.Coverage, offset, limit, filter)
	out.NextCursor = next
	for _, r := range page {
		row := CoverageRow{Ref: r.Ref.String(), Status: string(r.Status)}
		if wantReasons {
			row.Reasons = r.Reasons
		}
		if wantTests {
			for _, t := range r.Tests {
				row.Tests = append(row.Tests, CoverageTest{Test: t.Test, Result: t.Result})
			}
		}
		out.Coverage = append(out.Coverage, row)
	}
	return out, nil
}
