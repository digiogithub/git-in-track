package trace

import (
	"sort"
	"strings"
	"time"
)

// Test results (GIT-US-0115, ADR-037 section 7, docs/03 section 21.6): the
// last outcome of every test a run reported, normalized to the trace refs the
// graph uses, so the tests that verify a requirement can be answered from
// real runs. A report is parsed into RawResults (the report's own naming),
// a TestResolver maps each one to a repository path and symbol, and
// MatchRequirements joins the resolved results with the trace graph.
//
// Results are derived data: they live in a per-machine cache outside the
// repository (ResultStore) and are never written into a spec. Stamping
// `verified:` and computing the coverage state are the consumers' business
// (GIT-US-0116), as is the per-requirement verification cache verify.json
// (GIT-US-0141).

// ReportFormat names a test report format.
type ReportFormat string

// The supported formats.
const (
	FormatGoTest ReportFormat = "go"     // `go test -json` event stream
	FormatJUnit  ReportFormat = "junit"  // JUnit XML (testsuites/testsuite/testcase)
	FormatVitest ReportFormat = "vitest" // Vitest (and Jest) JSON reporter
)

// Valid reports whether f is one of the supported formats.
func (f ReportFormat) Valid() bool {
	switch f {
	case FormatGoTest, FormatJUnit, FormatVitest:
		return true
	}
	return false
}

// Outcome is the result of one test.
type Outcome string

// The three outcomes a report can give a test. A JUnit <error> is a fail;
// Vitest's pending, todo and disabled are skips.
const (
	OutcomePass Outcome = "pass"
	OutcomeFail Outcome = "fail"
	OutcomeSkip Outcome = "skip"
)

// rank orders outcomes for aggregation: failing beats passing beats skipped.
func (o Outcome) rank() int {
	switch o {
	case OutcomeFail:
		return 3
	case OutcomePass:
		return 2
	case OutcomeSkip:
		return 1
	}
	return 0
}

// worse returns the outcome that wins an aggregation of a and b.
func worse(a, b Outcome) Outcome {
	if b.rank() > a.rank() {
		return b
	}
	return a
}

// RawResult is one test as a report names it, before it is mapped to the
// repository.
type RawResult struct {
	Format ReportFormat
	// Scope is what the report groups the test under: the Go import path,
	// the JUnit classname, or the Vitest test file as written in the report.
	Scope string
	// File is a source file the report names for the test (the JUnit file
	// attribute, the Vitest test file), as written; empty when none.
	File string
	// Name is the test within its scope: "TestX/sub_case" for Go, the JUnit
	// testcase name, "describe > it" for Vitest.
	Name     string
	Outcome  Outcome
	Duration time.Duration
}

// ID is the report-level test id, "<scope>#<name>" (just the name when the
// report gives no scope). It identifies the test when it cannot be mapped to
// a file.
func (r RawResult) ID() string {
	if r.Scope == "" {
		return r.Name
	}
	return r.Scope + "#" + r.Name
}

// TestResult is the last result of one test, normalized to a trace ref.
type TestResult struct {
	// ID is the report-level id (RawResult.ID).
	ID     string       `json:"id"`
	Format ReportFormat `json:"format"`
	// Path is the repository-relative, "/"-separated test file; empty when
	// the report could not be mapped to a file of the working tree.
	Path string `json:"path,omitempty"`
	// Symbol is the trace-ref symbol: TestX/sub_case, "describe > it",
	// Class.test_method.
	Symbol string  `json:"symbol"`
	Result Outcome `json:"result"`
	// Duration is the run time in nanoseconds.
	Duration time.Duration `json:"durationNs"`
	// Commit is the full hex id of the commit the run was taken at; empty
	// when unknown.
	Commit string `json:"commit,omitempty"`
	// At is when the result was ingested (UTC).
	At time.Time `json:"at"`
	// Ambiguous lists the other files that could also be the test's file,
	// when the mapping had to choose (docs/07 section 4.19).
	Ambiguous []string `json:"ambiguous,omitempty"`
}

// TraceRef renders the result as "<path>#<symbol>", or the report-level id
// when the result is not mapped to a file.
func (r TestResult) TraceRef() string {
	if r.Path == "" {
		return r.ID
	}
	if r.Symbol == "" {
		return r.Path
	}
	return r.Path + "#" + r.Symbol
}

// key identifies the test across runs and formats: the trace ref when the
// result is mapped, so a JUnit run replaces the go test run of the same
// test, the format and id otherwise.
func (r TestResult) key() string {
	if r.Path != "" {
		return "ref:" + r.TraceRef()
	}
	return string(r.Format) + ":" + r.ID
}

// collapse aggregates repeated results of the same test in one report (a
// -count=2 run, a rerun, a test reported by two suites): failing beats
// passing beats skipped, and the longest duration is kept. The output is
// sorted by id, so a report always yields the same list.
func collapse(in []RawResult) []RawResult {
	byID := make(map[string]int, len(in))
	out := make([]RawResult, 0, len(in))
	for _, r := range in {
		id := r.ID()
		if i, ok := byID[id]; ok {
			out[i].Outcome = worse(out[i].Outcome, r.Outcome)
			if r.Duration > out[i].Duration {
				out[i].Duration = r.Duration
			}
			if out[i].File == "" {
				out[i].File = r.File
			}
			continue
		}
		byID[id] = len(out)
		out = append(out, r)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID() < out[j].ID() })
	return out
}

// symbolEncloses reports whether outer strictly encloses inner: TestX
// encloses TestX/sub, "d" encloses "d > it", Type encloses Type.Method.
func symbolEncloses(outer, inner string) bool {
	if outer == "" || len(inner) <= len(outer) {
		return false
	}
	for _, sep := range symbolSeps {
		if strings.HasPrefix(inner, outer+sep) {
			return true
		}
	}
	return false
}
