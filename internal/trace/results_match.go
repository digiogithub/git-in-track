package trace

import (
	"sort"
	"time"

	"github.com/digiogithub/git-in-track/internal/core"
)

// RequirementOutcome is what the ingested results say about one requirement.
// It is the raw evidence GIT-US-0116 turns into a coverage state; it is not
// that state (there is no "suspect" here, and nothing is stored).
type RequirementOutcome string

// The four outcomes. A requirement passes only when every linked test has a
// passing result and none failed (ADR-037 section 7).
const (
	RequirementPass     RequirementOutcome = "pass"     // every linked test passed
	RequirementFail     RequirementOutcome = "fail"     // some linked test failed
	RequirementPartial  RequirementOutcome = "partial"  // some passed, none failed, some have no result
	RequirementUntested RequirementOutcome = "untested" // no linked test, or none with a pass or fail
)

// Test-edge outcomes beyond the three of Outcome.
const (
	// OutcomeMissing is a linked test no ingested result matches.
	OutcomeMissing Outcome = "missing"
)

// How a result was matched to a trace ref.
const (
	MatchExact     = "exact"     // same path and symbol
	MatchEnclosed  = "enclosed"  // a sub-test or nested it of the traced symbol
	MatchEnclosing = "enclosing" // only the enclosing test reported (TestX for TestX/sub)
	MatchFile      = "file"      // a whole-file trace ref: every test of the file
)

// LinkedTestResult is the result of one test edge of a requirement.
type LinkedTestResult struct {
	// Test is the trace ref of the edge, "<path>[#<symbol>]".
	Test    string             `json:"test"`
	Sources []core.TraceSource `json:"sources"`
	// Result aggregates the matched results: fail beats pass beats skip;
	// missing when nothing matched.
	Result Outcome `json:"result"`
	// Match says how the results were matched; empty when missing.
	Match string `json:"match,omitempty"`
	// Results are the ids of the matched test results, sorted.
	Results []string `json:"results,omitempty"`
}

// RequirementResult is the aggregate of the results of a requirement's
// linked tests: the union of its Verifies: markers and trace.tests entries.
type RequirementResult struct {
	Ref    core.RequirementRef `json:"ref"`
	Result RequirementOutcome  `json:"result"`
	Tests  []LinkedTestResult  `json:"tests"`
	// Commits are the distinct commits of the matched results, sorted; more
	// than one means the evidence comes from several runs.
	Commits []string `json:"commits,omitempty"`
	// Latest is the newest ingest time of the matched results (UTC); zero
	// when nothing matched. It dates the evidence for coverage (GIT-US-0116).
	Latest time.Time `json:"-"`
}

// ResultSet indexes test results by file for matching. Only results mapped
// to a file can match a trace ref.
type ResultSet struct {
	byPath map[string][]TestResult
}

// NewResultSet indexes results.
func NewResultSet(results []TestResult) *ResultSet {
	s := &ResultSet{byPath: map[string][]TestResult{}}
	for _, r := range results {
		if r.Path != "" {
			s.byPath[r.Path] = append(s.byPath[r.Path], r)
		}
	}
	return s
}

// Match matches the results of one test edge (path and symbol of a trace
// ref). A whole-file ref takes every result of the file. A symbol ref takes
// the result of the same symbol and of every symbol it encloses (TestX takes
// TestX/sub); when there is none, it falls back to the nearest enclosing
// result (TestX/sub takes TestX, from a report that does not list sub-tests).
func (s *ResultSet) Match(p, symbol string) (results []TestResult, match string) {
	all := s.byPath[cleanPath(p)]
	if symbol == "" {
		if len(all) == 0 {
			return nil, ""
		}
		return all, MatchFile
	}
	var got []TestResult
	for _, r := range all {
		switch {
		case r.Symbol == symbol:
			got = append(got, r)
			if match == "" {
				match = MatchExact
			}
		case symbolEncloses(symbol, r.Symbol):
			got = append(got, r)
			if match != MatchExact {
				match = MatchEnclosed
			}
		}
	}
	if len(got) > 0 {
		return got, match
	}
	var best *TestResult
	for i, r := range all {
		if symbolEncloses(r.Symbol, symbol) && (best == nil || len(r.Symbol) > len(best.Symbol)) {
			best = &all[i]
		}
	}
	if best == nil {
		return nil, ""
	}
	return []TestResult{*best}, MatchEnclosing
}

// MatchRequirement aggregates the results of one requirement's linked tests.
func (s *ResultSet) MatchRequirement(tr core.TracedRequirement) RequirementResult {
	out := RequirementResult{Ref: tr.Ref, Tests: []LinkedTestResult{}}
	commits := map[string]bool{}
	pass, fail, none := 0, 0, 0
	for _, e := range tr.Tests {
		lt := LinkedTestResult{Test: e.TraceRef(), Sources: append([]core.TraceSource(nil), e.Sources...), Result: OutcomeMissing}
		got, match := s.Match(e.Path, e.Symbol)
		if len(got) > 0 {
			lt.Match = match
			lt.Result = ""
			for _, r := range got {
				lt.Result = worse(lt.Result, r.Result)
				lt.Results = append(lt.Results, r.ID)
				if r.Commit != "" {
					commits[r.Commit] = true
				}
				if r.At.After(out.Latest) {
					out.Latest = r.At
				}
			}
			sort.Strings(lt.Results)
		}
		switch lt.Result {
		case OutcomePass:
			pass++
		case OutcomeFail:
			fail++
		default:
			none++
		}
		out.Tests = append(out.Tests, lt)
	}
	switch {
	case fail > 0:
		out.Result = RequirementFail
	case pass == 0:
		out.Result = RequirementUntested
	case none > 0:
		out.Result = RequirementPartial
	default:
		out.Result = RequirementPass
	}
	for c := range commits {
		out.Commits = append(out.Commits, c)
	}
	sort.Strings(out.Commits)
	return out
}

// MatchRequirements aggregates the results of every requirement of the
// graph, sorted by spec and number.
func MatchRequirements(g *Graph, results []TestResult) []RequirementResult {
	s := NewResultSet(results)
	reqs := g.Requirements()
	out := make([]RequirementResult, 0, len(reqs))
	for _, tr := range reqs {
		out = append(out, s.MatchRequirement(tr))
	}
	return out
}
