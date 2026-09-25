package trace

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/digiogithub/git-in-track/internal/core"
	"github.com/digiogithub/git-in-track/internal/gitops"
)

// Requirement coverage (GIT-US-0116, ADR-037 section 7, docs/03 section
// 21.6): the computed state — untested, passing, failing, suspect — of each
// requirement, and the evidence a `verified:` stamp may copy. Coverage is the
// vault's coverage backend (vault.RequirementCoverage); its method set matches
// that interface without this package importing internal/vault.
//
// The evidence comes from an EvidenceSource. Today that is the test-result
// cache of GIT-US-0115 (ResultEvidence), which records per test and not per
// requirement, so it does not know which block rev was tested; the
// verification cache verify.json of GIT-US-0141 is a per-requirement source
// that does, and slots in behind the same interface. Nothing computed here is
// ever written into a file.

// Evidence is what a verification source knows about one requirement's
// linked tests.
type Evidence struct {
	// Result aggregates the linked tests: pass, fail, partial or untested.
	Result RequirementOutcome
	// Rev is the block rev that was tested; empty when the source does not
	// record it (the test-result cache).
	Rev core.Rev
	// Commits are the distinct commits the results were taken at, sorted.
	Commits []string
	// At is when the newest result was recorded (UTC); zero when none.
	At time.Time
	// By is who ran the tests; empty when the source does not record it.
	By string
	// Tests are the linked tests with their results.
	Tests []LinkedTestResult
}

// EvidenceSource answers the evidence of a set of requirements, one entry per
// requirement in the order given.
type EvidenceSource interface {
	Evidence(ctx context.Context, g *Graph, reqs []core.TracedRequirement) ([]Evidence, error)
}

// ResultEvidence is the test-result cache (ResultStore) as an evidence
// source. A nil Store is an empty cache.
type ResultEvidence struct {
	Store *ResultStore
}

// Evidence matches the cached results against each requirement's linked tests.
func (r ResultEvidence) Evidence(_ context.Context, _ *Graph, reqs []core.TracedRequirement) ([]Evidence, error) {
	var results []TestResult
	if r.Store != nil {
		var err error
		if results, _, err = r.Store.Load(); err != nil {
			return nil, err
		}
	}
	set := NewResultSet(results)
	out := make([]Evidence, 0, len(reqs))
	for _, tr := range reqs {
		rr := set.MatchRequirement(tr)
		out = append(out, Evidence{Result: rr.Result, Commits: rr.Commits, At: rr.Latest, Tests: rr.Tests})
	}
	return out, nil
}

// ErrUnknownCommit is a commit the history does not hold (a shallow clone, a
// rewritten branch): nothing can be said about what changed since.
var ErrUnknownCommit = errors.New("unknown commit")

// ChangeLister lists what changed between a commit and the working tree, in
// the shape the trace graph's reverse query takes. A commit the history does
// not hold fails with ErrUnknownCommit.
type ChangeLister interface {
	ChangedSince(ctx context.Context, commit string) ([]core.TraceChange, error)
}

// GitChanges is a ChangeLister over a gitops backend (GIT-US-0112).
//
// It compares against the working tree rather than HEAD: the trace graph is
// scanned from the working tree, so the changed lines have to be counted on
// the same side for them to map to the right symbols, and a traced symbol
// edited but not committed yet no longer holds what the evidence verified
// either. On a clean tree the two are the same.
type GitChanges struct {
	Backend gitops.Backend
}

// ChangedSince lists the changes between commit and the working tree.
func (g GitChanges) ChangedSince(ctx context.Context, commit string) ([]core.TraceChange, error) {
	changes, err := g.Backend.ChangedFiles(ctx, commit, gitops.WorkingTree)
	if err != nil {
		var ge *gitops.Error
		if errors.As(err, &ge) && ge.Code == gitops.CodeUnknownRevision {
			return nil, fmt.Errorf("%w: %s", ErrUnknownCommit, commit)
		}
		return nil, fmt.Errorf("changes since %s: %w", commit, err)
	}
	out := make([]core.TraceChange, 0, len(changes))
	for _, c := range changes {
		tc := core.TraceChange{Path: c.Path, OldPath: c.OldPath}
		for _, l := range c.Lines {
			tc.Lines = append(tc.Lines, core.LineSpan{Start: l.Start, Count: l.Count})
		}
		out = append(out, tc)
	}
	return out, nil
}

// Coverage computes requirement coverage over a trace Engine.
type Coverage struct {
	engine   *Engine
	evidence EvidenceSource
	changes  ChangeLister
}

// NewCoverage returns the coverage backend of one working tree. A nil
// evidence source is an empty test-result cache; a nil change lister (a
// repository without history) leaves code drift unchecked.
func NewCoverage(engine *Engine, evidence EvidenceSource, changes ChangeLister) *Coverage {
	if evidence == nil {
		evidence = ResultEvidence{}
	}
	return &Coverage{engine: engine, evidence: evidence, changes: changes}
}

// traced returns the trace of each requirement and their evidence.
func (c *Coverage) traced(ctx context.Context, ix *core.Index, reqs []core.RequirementView) ([]core.TracedRequirement, []Evidence, error) {
	g, err := c.engine.Graph(ctx, ix)
	if err != nil {
		return nil, nil, err
	}
	traced := make([]core.TracedRequirement, 0, len(reqs))
	for _, v := range reqs {
		tr, ok := g.Requirement(v.Ref)
		if !ok {
			tr = core.TracedRequirement{Ref: v.Ref}
		}
		traced = append(traced, tr)
	}
	evs, err := c.evidence.Evidence(ctx, g, traced)
	if err != nil {
		return nil, nil, fmt.Errorf("read the verification evidence: %w", err)
	}
	if len(evs) != len(traced) {
		return nil, nil, fmt.Errorf("the evidence source answered %d of %d requirements", len(evs), len(traced))
	}
	return traced, evs, nil
}

// Coverage returns one row per requirement, in the order given.
func (c *Coverage) Coverage(ctx context.Context, ix *core.Index, reqs []core.RequirementView) ([]core.CoverageRow, error) {
	traced, evs, err := c.traced(ctx, ix, reqs)
	if err != nil {
		return nil, err
	}
	d := &drift{ctx: ctx, ix: ix, c: c, byCommit: map[string]map[core.RequirementRef][]core.TraceHit{}}
	rows := make([]core.CoverageRow, 0, len(reqs))
	for i, v := range reqs {
		ref := v.Ref
		var since DriftFunc
		if c.changes != nil {
			since = func(commit string) ([]string, error) { return d.reasons(commit, ref) }
		}
		row, err := Classify(CoverageInput{
			Ref: ref, BlockRev: v.BlockRev, Stamp: v.Verified, Linked: len(traced[i].Tests), Evidence: evs[i],
		}, since)
		if err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// StampEvidence returns the stamp each requirement's evidence allows: every
// linked test passed, at one known commit, on the text the requirement holds
// now (when the source records the tested rev). The stamp's rev is the
// requirement's current block rev, its at the newest result's time.
func (c *Coverage) StampEvidence(ctx context.Context, ix *core.Index, reqs []core.RequirementView) ([]core.StampEvidence, error) {
	traced, evs, err := c.traced(ctx, ix, reqs)
	if err != nil {
		return nil, err
	}
	out := make([]core.StampEvidence, 0, len(reqs))
	for i, v := range reqs {
		ev := evs[i]
		se := core.StampEvidence{Ref: v.Ref}
		switch {
		case len(traced[i].Tests) == 0:
			se.Reason = core.CoverageReasonNoTests
		case ev.Result == RequirementFail:
			se.Reason = core.CoverageReasonFailed
		case ev.Result == RequirementPartial:
			se.Reason = core.CoverageReasonPartial
		case ev.Result != RequirementPass:
			se.Reason = core.CoverageReasonNoResults
		case ev.Rev != "" && ev.Rev != v.BlockRev:
			se.Reason = core.CoverageReasonText
		case len(ev.Commits) == 0:
			se.Reason = core.StampReasonNoCommit
		case len(ev.Commits) > 1:
			se.Reason = core.StampReasonMixedCommits
		default:
			se.Verified = &core.Verification{
				Rev: v.BlockRev, Commit: ev.Commits[0], At: core.NewTimestamp(ev.At), By: ev.By,
			}
		}
		out = append(out, se)
	}
	return out, nil
}

// drift memoizes, per evidence commit, which trace edges changed since.
type drift struct {
	ctx      context.Context //nolint:containedctx // lives for one Coverage call
	ix       *core.Index
	c        *Coverage
	byCommit map[string]map[core.RequirementRef][]core.TraceHit
	unknown  map[string]bool
}

// reasons returns the drift reasons of ref since commit: nil when nothing it
// traces changed, ErrUnknownCommit when the history does not hold commit.
func (d *drift) reasons(commit string, ref core.RequirementRef) ([]string, error) {
	if d.unknown[commit] {
		return nil, ErrUnknownCommit
	}
	hits, ok := d.byCommit[commit]
	if !ok {
		changes, err := d.c.changes.ChangedSince(d.ctx, commit)
		if errors.Is(err, ErrUnknownCommit) {
			if d.unknown == nil {
				d.unknown = map[string]bool{}
			}
			d.unknown[commit] = true
			return nil, ErrUnknownCommit
		}
		if err != nil {
			return nil, fmt.Errorf("list the changes since %s: %w", commit, err)
		}
		hits = map[core.RequirementRef][]core.TraceHit{}
		if len(changes) > 0 {
			all, err := d.c.engine.TraceTouching(d.ctx, d.ix, changes)
			if err != nil {
				return nil, err
			}
			for _, h := range all {
				hits[h.Ref] = append(hits[h.Ref], h)
			}
		}
		d.byCommit[commit] = hits
	}
	var out []string
	for _, h := range hits[ref] {
		prefix := core.CoverageReasonCode
		if h.Role == core.TraceRoleTest {
			prefix = core.CoverageReasonTest
		}
		out = append(out, prefix+h.TraceRef())
	}
	return out, nil
}

// CoverageInput is what Classify decides one requirement's state from.
type CoverageInput struct {
	Ref core.RequirementRef
	// BlockRev is the requirement's current block rev.
	BlockRev core.Rev
	// Stamp is its verified: stamp, nil when it has none.
	Stamp *core.Verification
	// Linked is the number of linked tests.
	Linked   int
	Evidence Evidence
}

// DriftFunc returns the drift reasons of a requirement since a commit: empty
// when nothing it traces changed, ErrUnknownCommit when the commit is not in
// the history. A nil DriftFunc leaves drift unchecked.
type DriftFunc func(commit string) ([]string, error)

// maxDriftReasons caps the changed trace refs a row lists; the rest are
// counted in a "+N" reason, so a row stays small.
const maxDriftReasons = 3

// Classify computes the coverage state of one requirement (docs/03 section
// 21.6, R-REQ-12):
//
//   - the evidence is the newest run — a pass or a fail recorded after the
//     stamp's at, on the current text when the source records the text —
//     else the stamp;
//   - a failing run → failing;
//   - a stamp whose rev is not the current block rev, with no newer run of
//     the current text → suspect ("text");
//   - passing evidence whose traced code or tests changed between its commit
//     and the working tree → suspect ("code:<ref>", "test:<ref>"), and one
//     whose commit the history does not hold → suspect ("commit-unknown");
//   - other passing evidence → passing;
//   - no evidence → untested ("no-tests", "no-results" or "partial").
//
// A partial run — some linked tests passed, none failed, some have no
// result — is not a pass: it never counts as evidence, and it adds "partial"
// to whatever the stamp decides. The reasons also say which evidence decided:
// "results" or "stamp".
func Classify(in CoverageInput, driftSince DriftFunc) (core.CoverageRow, error) {
	ev := in.Evidence
	row := core.CoverageRow{Ref: in.Ref}
	for _, t := range ev.Tests {
		row.Tests = append(row.Tests, core.CoverageTest{Test: t.Test, Result: string(t.Result)})
	}
	var stampAt time.Time
	if in.Stamp != nil {
		stampAt = in.Stamp.At.Time
	}
	ran := ev.Result == RequirementPass || ev.Result == RequirementFail
	fresh := ran && (in.Stamp == nil || ev.At.After(stampAt)) && (ev.Rev == "" || ev.Rev == in.BlockRev)
	freshText := fresh && ev.Rev == in.BlockRev
	stampText := in.Stamp != nil && in.Stamp.Rev != "" && in.Stamp.Rev != in.BlockRev

	switch {
	case fresh && ev.Result == RequirementFail:
		row.Status = core.CoverageFailing
		row.Reasons = []string{core.CoverageReasonFailed, core.CoverageReasonResults}
	case stampText && !freshText:
		row.Status = core.CoverageSuspect
		row.Reasons = []string{core.CoverageReasonText, core.CoverageReasonStamp}
	case fresh:
		if err := classifyDrift(&row, ev.Commits, driftSince); err != nil {
			return core.CoverageRow{}, err
		}
		row.Reasons = append(row.Reasons, core.CoverageReasonResults)
	case in.Stamp != nil:
		var commits []string
		if in.Stamp.Commit != "" {
			commits = []string{in.Stamp.Commit}
		}
		if err := classifyDrift(&row, commits, driftSince); err != nil {
			return core.CoverageRow{}, err
		}
		row.Reasons = append(row.Reasons, core.CoverageReasonStamp)
	default:
		row.Status = core.CoverageUntested
		switch {
		case in.Linked == 0:
			row.Reasons = []string{core.CoverageReasonNoTests}
		case ev.Result == RequirementPartial:
			row.Reasons = []string{core.CoverageReasonPartial}
		default:
			row.Reasons = []string{core.CoverageReasonNoResults}
		}
		return row, nil
	}
	if ev.Result == RequirementPartial {
		row.Reasons = append(row.Reasons, core.CoverageReasonPartial)
	}
	return row, nil
}

// classifyDrift sets a passing row to passing or suspect from the drift since
// each of the evidence's commits.
func classifyDrift(row *core.CoverageRow, commits []string, driftSince DriftFunc) error {
	row.Status = core.CoveragePassing
	if len(commits) == 0 || driftSince == nil {
		row.Reasons = append(row.Reasons, core.CoverageReasonUnchecked)
		return nil
	}
	seen := map[string]bool{}
	var changed []string
	for _, commit := range commits {
		reasons, err := driftSince(commit)
		if errors.Is(err, ErrUnknownCommit) {
			row.Status = core.CoverageSuspect
			if !seen[core.CoverageReasonCommitUnknown] {
				seen[core.CoverageReasonCommitUnknown] = true
				row.Reasons = append(row.Reasons, core.CoverageReasonCommitUnknown)
			}
			continue
		}
		if err != nil {
			return err
		}
		for _, r := range reasons {
			if !seen[r] {
				seen[r] = true
				changed = append(changed, r)
			}
		}
	}
	if len(changed) == 0 {
		return nil
	}
	sort.Strings(changed)
	row.Status = core.CoverageSuspect
	if len(changed) > maxDriftReasons {
		more := len(changed) - maxDriftReasons
		changed = append(changed[:maxDriftReasons:maxDriftReasons], core.CoverageReasonMore+strconv.Itoa(more))
	}
	row.Reasons = append(row.Reasons, changed...)
	return nil
}
