package core

// The answer types of requirement coverage (ADR-037 section 7, docs/03 section
// 21.6, GIT-US-0116). Coverage is computed by a native host — it needs test
// results, the trace graph and git history — so only the shapes a host hands
// back through the vault seam live here, free of any filesystem access. None
// of them is ever written into a file: the coverage state is derived, and a
// writer MUST NOT store a key named suspect, coverage or tested.

// CoverageStatus is the computed coverage state of one requirement.
type CoverageStatus string

// The four states.
const (
	// CoverageUntested: no linked test, or no evidence that they ran.
	CoverageUntested CoverageStatus = "untested"
	// CoveragePassing: the evidence passed and nothing it covered changed since.
	CoveragePassing CoverageStatus = "passing"
	// CoverageFailing: a linked test failed in the newest evidence.
	CoverageFailing CoverageStatus = "failing"
	// CoverageSuspect: the evidence passed, but the requirement's text or its
	// traced code or tests changed since.
	CoverageSuspect CoverageStatus = "suspect"
)

// Valid reports whether s is one of the four states.
func (s CoverageStatus) Valid() bool {
	switch s {
	case CoverageUntested, CoveragePassing, CoverageFailing, CoverageSuspect:
		return true
	}
	return false
}

// Coverage reasons: short codes, so a row stays cheap for an agent to read.
// A code-drift reason carries the trace ref it names after a colon.
const (
	CoverageReasonNoTests       = "no-tests"       // no Verifies: marker or trace.tests entry
	CoverageReasonNoResults     = "no-results"     // linked tests, none with a result
	CoverageReasonPartial       = "partial"        // some linked tests passed, some have no result
	CoverageReasonFailed        = "failed"         // a linked test failed
	CoverageReasonText          = "text"           // the block rev differs from verified.rev
	CoverageReasonCode          = "code:"          // + trace ref: traced code changed since the evidence commit
	CoverageReasonTest          = "test:"          // + trace ref: a traced test changed since the evidence commit
	CoverageReasonMore          = "+"              // + count: more changed trace refs than listed
	CoverageReasonCommitUnknown = "commit-unknown" // the evidence commit is not in this history
	CoverageReasonUnchecked     = "unchecked"      // no history to compare with: drift not checked
	CoverageReasonStamp         = "stamp"          // the evidence is the verified stamp
	CoverageReasonResults       = "results"        // the evidence is the local test results
)

// CoverageTest is one linked test of a requirement with its latest result:
// pass, fail, skip, or missing when no result matched it.
type CoverageTest struct {
	Test   string `json:"test"`
	Result string `json:"result"`
}

// CoverageRow is the coverage of one requirement: one compact row, the shape
// every surface (vault, CLI, MCP) returns.
type CoverageRow struct {
	Ref     RequirementRef `json:"ref"`
	Status  CoverageStatus `json:"status"`
	Reasons []string       `json:"reasons,omitempty"`
	Tests   []CoverageTest `json:"tests,omitempty"`
}

// StampEvidence is what a coverage host offers a stamp writer for one
// requirement: the stamp it may write, or the reason it may not.
type StampEvidence struct {
	Ref RequirementRef `json:"ref"`
	// Verified is the stamp to write; nil when the evidence does not allow one.
	// Its By may be empty, when the evidence does not record who ran it.
	Verified *Verification `json:"verified,omitempty"`
	// Reason says why there is no stamp: a coverage reason code, or
	// "mixed-commits" when the passing results come from several commits.
	Reason string `json:"reason,omitempty"`
}

// StampReasonMixedCommits is a passing requirement whose linked tests passed
// at different commits: no single commit can be stamped.
const StampReasonMixedCommits = "mixed-commits"

// StampReasonNoCommit is passing evidence recorded without a full commit id.
const StampReasonNoCommit = "no-commit"

// ValidCommitID reports whether s is a full hex commit id (40 or 64 hex
// digits), the only form a verified.commit may hold.
func ValidCommitID(s string) bool { return commitRE.MatchString(s) }
