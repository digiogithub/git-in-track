package trace

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/digiogithub/git-in-track/internal/core"
	"github.com/digiogithub/git-in-track/internal/core/osfs"
)

// The verification cache verify.json (GIT-US-0141, ADR-037 section 7, docs/03
// R-REQ-11): one entry per requirement per recorded run, beside index.json in
// each project's .pmngr folder. It sits between the two other layers and
// duplicates neither:
//
//   - the test-result cache (ResultStore) keeps the raw last result of every
//     test, per machine and outside the repository — the input;
//   - verify.json keeps, per requirement, what a run said about it and which
//     block rev it tested — the evidence coverage and the stamp read;
//   - the `verified:` stamp in the spec is the durable, committed baseline.
//
// `gintrack spec ingest` writes both caches: the test results first, then an
// entry for every requirement a fresh result touches (VerificationEntries).
// VerifyEvidence reads the entries back for coverage and for the stamps.

// VerifyEvidence is the verification cache as an evidence source: for each
// requirement, the newest entry of its current block rev, else its newest
// entry at any rev — which then does not count, because it tested another
// text. A requirement without an entry has no evidence, and coverage falls
// back to its stamp (R-REQ-12). A missing or corrupt cache is empty.
type VerifyEvidence struct {
	// FS is the working tree the projects' .pmngr folders are read from; nil
	// is an empty cache.
	FS core.FS
	// Open returns the cache of one project; nil means its verify.json on FS.
	// The browser, where the cache is an IndexedDB record, passes its own.
	Open func(p core.ProjectRef) core.VerifyCache
}

// Evidence answers the verification cache's entries for each requirement.
func (e VerifyEvidence) Evidence(_ context.Context, q EvidenceQuery) ([]Evidence, error) {
	loaded := map[core.ProjectKey][]core.VerifyEntry{}
	entriesOf := func(ref core.RequirementRef) ([]core.VerifyEntry, error) {
		key, _, _, err := core.ParseItemID(string(ref.Spec))
		if err != nil || q.Index == nil {
			return nil, nil //nolint:nilerr // a malformed ref has no evidence
		}
		if es, ok := loaded[key]; ok {
			return es, nil
		}
		var es []core.VerifyEntry
		if p, ok := q.Index.Project(key); ok {
			if cache := e.open(p); cache != nil {
				// A corrupt cache reads as empty: derived data is never fatal.
				if es, _, err = cache.Load(); err != nil {
					return nil, fmt.Errorf("read the verification cache of %s: %w", key, err)
				}
			}
		}
		loaded[key] = es
		return es, nil
	}
	out := make([]Evidence, 0, len(q.Reqs))
	for i, tr := range q.Reqs {
		var rev core.Rev
		if i < len(q.BlockRevs) {
			rev = q.BlockRevs[i]
		}
		es, err := entriesOf(tr.Ref)
		if err != nil {
			return nil, err
		}
		entry, ok := core.LatestVerifyEntry(es, tr.Ref, rev)
		if !ok || rev == "" {
			entry, ok = core.LatestVerifyEntry(es, tr.Ref, "")
		}
		if !ok {
			out = append(out, missingEvidence(tr))
			continue
		}
		out = append(out, entryEvidence(entry))
	}
	return out, nil
}

// open returns the cache of one project.
func (e VerifyEvidence) open(p core.ProjectRef) core.VerifyCache {
	if e.Open != nil {
		return e.Open(p)
	}
	if e.FS == nil {
		return nil
	}
	return core.NewFileVerifyCache(e.FS, p.BacklogPath)
}

// missingEvidence is a requirement no entry records: untested, every linked
// test missing.
func missingEvidence(tr core.TracedRequirement) Evidence {
	ev := Evidence{Result: RequirementUntested}
	for _, t := range tr.Tests {
		ev.Tests = append(ev.Tests, LinkedTestResult{
			Test: t.TraceRef(), Sources: append([]core.TraceSource(nil), t.Sources...), Result: OutcomeMissing,
		})
	}
	return ev
}

// entryEvidence turns a cache entry into evidence.
func entryEvidence(e core.VerifyEntry) Evidence {
	ev := Evidence{
		Result:      RequirementOutcome(e.Result),
		Rev:         e.Rev,
		Commits:     e.AllCommits(),
		Uncommitted: e.Uncommitted,
		At:          e.At,
		By:          e.By,
	}
	for _, t := range e.Tests {
		ev.Tests = append(ev.Tests, LinkedTestResult{Test: t.Test, Result: Outcome(t.Result)})
	}
	return ev
}

// VerificationEntries derives the verification-cache entries of an ingest:
// one for every requirement a fresh result touches — at least one of its
// linked tests matched a result of this run — aggregating the latest results
// of all its linked tests (all), recorded against the block rev the
// requirement holds now. A requirement whose aggregate is untested gets no
// entry, and neither does one the index does not hold.
func VerificationEntries(ix *core.Index, g *Graph, all, fresh []TestResult, by string) []core.VerifyEntry {
	if ix == nil || g == nil || len(fresh) == 0 {
		return nil
	}
	freshSet, allSet := NewResultSet(fresh), NewResultSet(all)
	var out []core.VerifyEntry
	for _, tr := range g.Requirements() {
		if !touched(freshSet.MatchRequirement(tr)) {
			continue
		}
		rr := allSet.MatchRequirement(tr)
		result := core.VerifyResult(rr.Result)
		if !result.Valid() {
			continue
		}
		view, err := ix.Requirement(tr.Ref)
		if err != nil {
			continue
		}
		e := core.VerifyEntry{
			Ref: tr.Ref, Rev: view.BlockRev, Uncommitted: rr.Uncommitted,
			Result: result, At: rr.Latest.UTC().Truncate(time.Second), By: by,
			Tests: make([]core.VerifyTest, 0, len(rr.Tests)),
		}
		if len(rr.Commits) == 1 {
			e.Commit = rr.Commits[0]
		} else {
			e.Commits = rr.Commits
		}
		for _, t := range rr.Tests {
			e.Tests = append(e.Tests, core.VerifyTest{Test: t.Test, Result: string(t.Result)})
		}
		out = append(out, e)
	}
	return out
}

// touched reports whether a fresh result matched any linked test.
func touched(rr RequirementResult) bool {
	for _, t := range rr.Tests {
		if t.Result != OutcomeMissing {
			return true
		}
	}
	return false
}

// VerifyRecord is what recording an ingest into one project's
// verification cache changed.
type VerifyRecord struct {
	Project core.ProjectKey `json:"project"`
	// Path is the cache file, repository-relative.
	Path string `json:"path"`
	core.VerifyRecordStats
}

// RepositoryTrace builds the backlog index and the trace graph of the
// repository at root from scratch, like RepositoryGraph, and also returns the
// file system they were read from. It returns nil values and no error when
// the repository holds no backlog.
func RepositoryTrace(ctx context.Context, root string) (core.FS, *core.Index, *Graph, error) {
	fsys, err := osfs.New(root)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("open %s: %w", root, err)
	}
	projects, err := core.DiscoverProjects(fsys, ".")
	if err != nil {
		return nil, nil, nil, fmt.Errorf("discover the projects of %s: %w", root, err)
	}
	if len(projects) == 0 {
		return nil, nil, nil, nil
	}
	ix := core.NewIndex(fsys, projects)
	if _, err := ix.Build(ctx, true); err != nil {
		return nil, nil, nil, fmt.Errorf("index %s: %w", root, err)
	}
	c, err := Scan(ctx, root, Options{})
	if err != nil {
		return nil, nil, nil, err
	}
	g, err := BuildGraph(ix, c.Markers(), os.DirFS(root))
	if err != nil {
		return nil, nil, nil, err
	}
	return fsys, ix, g, nil
}

// RecordVerification writes entries into the verification cache of the
// project each belongs to, and reports one record per project written.
func RecordVerification(fsys core.FS, ix *core.Index, entries []core.VerifyEntry) ([]VerifyRecord, error) {
	byKey := map[core.ProjectKey][]core.VerifyEntry{}
	for _, e := range entries {
		key, _, _, err := core.ParseItemID(string(e.Ref.Spec))
		if err != nil {
			continue
		}
		byKey[key] = append(byKey[key], e)
	}
	out := []VerifyRecord{}
	for _, p := range ix.Projects() {
		fresh := byKey[p.Key]
		if len(fresh) == 0 {
			continue
		}
		cache := core.NewFileVerifyCache(fsys, p.BacklogPath)
		st, err := cache.Record(fresh)
		if err != nil {
			return nil, fmt.Errorf("record the verification cache of %s: %w", p.Key, err)
		}
		out = append(out, VerifyRecord{Project: p.Key, Path: cache.Path(), VerifyRecordStats: st})
	}
	return out, nil
}
