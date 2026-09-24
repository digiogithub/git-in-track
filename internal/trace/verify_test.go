package trace

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/digiogithub/git-in-track/internal/core"
	"github.com/digiogithub/git-in-track/internal/core/osfs"
	"github.com/digiogithub/git-in-track/internal/gitops"
)

// verifyPath is the verification cache of the coverage fixture's project.
const verifyPath = "docs/.pmngr/verify.json"

// newVerifyRepo is the coverage fixture with the evidence source the hosts
// install: the verification cache verify.json (GIT-US-0141). The test-result
// cache stays the raw input `gintrack spec ingest` derives entries from.
func newVerifyRepo(t *testing.T) *covRepo {
	t.Helper()
	r := newCovRepo(t)
	fsys, err := osfs.New(r.root)
	if err != nil {
		t.Fatal(err)
	}
	backend, err := gitops.Open(r.root, gitops.Options{Backend: gitops.KindGoGit})
	if err != nil {
		t.Fatal(err)
	}
	engine := NewEngine(r.root, EngineOptions{})
	r.vlt.SetRequirementTracer(engine)
	r.vlt.SetRequirementCoverage(NewCoverage(engine, VerifyEvidence{FS: fsys}, GitChanges{Backend: backend}))
	return r
}

// record is what `gintrack spec ingest` does: merge the results into the
// test-result cache, then record the requirements they touch in verify.json.
func (r *covRepo) record(commit string, hours int, outcomes map[string]Outcome) []VerifyRecord {
	r.t.Helper()
	var fresh []TestResult
	for symbol, o := range outcomes {
		fresh = append(fresh, TestResult{
			ID: "example.com/acme/src#" + symbol, Format: FormatGoTest, Path: "src/alloc_test.go",
			Symbol: symbol, Result: o, Commit: commit, At: r.base.Add(time.Duration(hours) * time.Hour),
		})
	}
	if _, err := r.store.Merge(r.root, fresh); err != nil {
		r.t.Fatal(err)
	}
	all, _, err := r.store.Load()
	if err != nil {
		r.t.Fatal(err)
	}
	fsys, ix, g, err := RepositoryTrace(context.Background(), r.root)
	if err != nil {
		r.t.Fatal(err)
	}
	recs, err := RecordVerification(fsys, ix, VerificationEntries(ix, g, all, fresh, "ci"))
	if err != nil {
		r.t.Fatal(err)
	}
	return recs
}

func (r *covRepo) verifyEntries() []core.VerifyEntry {
	r.t.Helper()
	entries, corrupt := core.DecodeVerifyCache([]byte(r.read(verifyPath)))
	if corrupt {
		r.t.Fatalf("verify.json is corrupt:\n%s", r.read(verifyPath))
	}
	return entries
}

func checkRow(t *testing.T, rows map[string]core.CoverageRow, ref string, w want) {
	t.Helper()
	row := rows[ref]
	if row.Status != w.status || strings.Join(row.Reasons, ",") != strings.Join(w.reasons, ",") {
		t.Errorf("%s = %s %v, want %s %v", ref, row.Status, row.Reasons, w.status, w.reasons)
	}
}

// TestVerifyCacheEvidence walks the verification cache through the cases of
// GIT-US-0141: a missing cache, entries recorded by an ingest, precedence of
// a newer entry over the stamp, a deleted cache falling back to the stamp
// (the raw test results are not evidence on their own), and a corrupt cache
// that reads as empty and is rebuilt by the next run.
func TestVerifyCacheEvidence(t *testing.T) {
	r := newVerifyRepo(t)
	c1 := r.commit("base")

	t.Run("missing cache", func(t *testing.T) {
		rows := r.coverage()
		checkRow(t, rows, "ACME-SP-0001.R1", want{core.CoverageUntested, []string{"no-results"}})
		checkRow(t, rows, "ACME-SP-0001.R3", want{core.CoverageUntested, []string{"no-tests"}})
		if tests := rows["ACME-SP-0001.R1"].Tests; len(tests) != 1 || tests[0].Result != "missing" {
			t.Errorf("R1 tests = %+v, want its linked test missing", tests)
		}
	})

	t.Run("an ingest records the touched requirements", func(t *testing.T) {
		recs := r.record(c1, 1, map[string]Outcome{"TestNextID": OutcomePass, "TestFormat": OutcomePass})
		if len(recs) != 1 || recs[0].Project != "ACME" || recs[0].Path != verifyPath || recs[0].Added != 2 || recs[0].Rebuilt {
			t.Fatalf("records = %+v, want 2 entries added to %s", recs, verifyPath)
		}
		entries := r.verifyEntries()
		if len(entries) != 2 {
			t.Fatalf("entries = %+v, want R1 and R2", entries)
		}
		e := entries[0]
		if e.Ref.String() != "ACME-SP-0001.R1" || e.Result != core.VerifyPass || e.Commit != c1 || e.By != "ci" ||
			!strings.HasPrefix(string(e.Rev), "sha256:") || len(e.Tests) != 1 || e.Tests[0].Result != "pass" {
			t.Errorf("R1 entry = %+v", e)
		}
		rows := r.coverage()
		checkRow(t, rows, "ACME-SP-0001.R1", want{core.CoveragePassing, []string{"results"}})
		if rows["ACME-SP-0001.R1"].Commit != c1 {
			t.Errorf("R1 commit = %q, want %s", rows["ACME-SP-0001.R1"].Commit, c1)
		}
	})

	t.Run("a newer entry takes precedence over the stamp", func(t *testing.T) {
		got := r.stamp("ACME-SP-0001.R1")
		if len(got.Stamped) != 1 || got.Stamped[0].Verified.Commit != c1 || got.Stamped[0].Verified.By != "ci" {
			t.Fatalf("stamp = %+v, want R1 stamped at %s by ci", got, c1)
		}
		checkRow(t, r.coverage(), "ACME-SP-0001.R1", want{core.CoveragePassing, []string{"stamp"}})
		r.record(c1, 2, map[string]Outcome{"TestNextID": OutcomeFail})
		checkRow(t, r.coverage(), "ACME-SP-0001.R1", want{core.CoverageFailing, []string{"failed", "results"}})
		if again := r.stamp("ACME-SP-0001.R1"); len(again.Unstamped) != 1 || again.Unstamped[0].Reason != "failed" {
			t.Errorf("stamp after a failure = %+v, want unstamped: failed", again)
		}
	})

	t.Run("a deleted cache falls back to the stamp", func(t *testing.T) {
		if err := os.Remove(filepath.Join(r.root, verifyPath)); err != nil {
			t.Fatal(err)
		}
		// The failing result is still in the test-result cache, but coverage
		// reads only verify.json: the stamp is the last durable state.
		rows := r.coverage()
		checkRow(t, rows, "ACME-SP-0001.R1", want{core.CoveragePassing, []string{"stamp"}})
		checkRow(t, rows, "ACME-SP-0001.R2", want{core.CoverageUntested, []string{"no-results"}})
	})

	t.Run("a corrupt cache reads as empty and is rebuilt", func(t *testing.T) {
		r.write(verifyPath, "{not json")
		checkRow(t, r.coverage(), "ACME-SP-0001.R1", want{core.CoveragePassing, []string{"stamp"}})
		r.write(verifyPath, `{"version": 99, "entries": []}`)
		checkRow(t, r.coverage(), "ACME-SP-0001.R1", want{core.CoveragePassing, []string{"stamp"}})
		recs := r.record(c1, 3, map[string]Outcome{"TestFormat": OutcomePass})
		if len(recs) != 1 || !recs[0].Rebuilt || recs[0].Total != 1 {
			t.Fatalf("records = %+v, want the file rebuilt with R2", recs)
		}
		if entries := r.verifyEntries(); len(entries) != 1 || entries[0].Ref.String() != "ACME-SP-0001.R2" {
			t.Errorf("entries = %+v, want R2 only", entries)
		}
		checkRow(t, r.coverage(), "ACME-SP-0001.R2", want{core.CoveragePassing, []string{"results"}})
	})

	t.Run("an entry of another text does not count", func(t *testing.T) {
		r.record(c1, 4, map[string]Outcome{"TestNextID": OutcomePass})
		r.editR1()
		checkRow(t, r.coverage(), "ACME-SP-0001.R1", want{core.CoverageSuspect, []string{"text", "stamp"}})
		if got := r.stamp("ACME-SP-0001.R1"); len(got.Unstamped) != 1 || got.Unstamped[0].Reason != "text" {
			t.Errorf("stamp of the edited text = %+v, want unstamped: text", got)
		}
		r.record(c1, 5, map[string]Outcome{"TestNextID": OutcomePass})
		checkRow(t, r.coverage(), "ACME-SP-0001.R1", want{core.CoveragePassing, []string{"results"}})
		if got := r.stamp("ACME-SP-0001.R1"); len(got.Stamped) != 1 {
			t.Errorf("stamp after re-running on the new text = %+v, want stamped", got)
		}
	})
}

// doneResult is the part of an item.move result the done stamp fills.
type doneResult struct {
	SpecDelta *struct {
		Stamped []struct {
			Ref      string            `json:"ref"`
			Verified core.Verification `json:"verified"`
		} `json:"stamped"`
		Unstamped []struct {
			Ref    string `json:"ref"`
			Reason string `json:"reason"`
		} `json:"unstamped"`
	} `json:"specDelta"`
}

// story creates a story in todo that implements refs.
func (r *covRepo) story(title string, refs ...string) string {
	r.t.Helper()
	links := make([]map[string]string, 0, len(refs))
	for _, ref := range refs {
		links = append(links, map[string]string{"kind": "implements", "target": ref})
	}
	var got struct {
		Item struct {
			ID string `json:"id"`
		} `json:"item"`
	}
	raw := r.call("item.create", map[string]any{
		"project": "ACME", "type": "story", "title": title, "status": "todo",
		"assignees": []string{"marta"}, "links": links,
	})
	if err := json.Unmarshal(raw, &got); err != nil {
		r.t.Fatal(err)
	}
	return got.Item.ID
}

// done moves an item to done and returns what the transition stamped.
func (r *covRepo) done(id string) doneResult {
	r.t.Helper()
	var item struct {
		Rev string `json:"rev"`
	}
	if err := json.Unmarshal(r.call("item.get", map[string]any{"id": id}), &item); err != nil {
		r.t.Fatal(err)
	}
	var got doneResult
	if err := json.Unmarshal(r.call("item.move", map[string]any{"id": id, "status": "done", "rev": item.Rev}), &got); err != nil {
		r.t.Fatal(err)
	}
	if got.SpecDelta == nil {
		r.t.Fatalf("move of %s to done reported no specDelta", id)
	}
	return got
}

// TestDoneTransitionStamps is the end-to-end stamp moment of R-REQ-11a (a)
// over a real repository: a story moving to done stamps the requirements it
// implements from the verification cache, in the same write — and only the
// ones whose linked tests all passed. A story without evidence still moves.
func TestDoneTransitionStamps(t *testing.T) {
	r := newVerifyRepo(t)
	c1 := r.commit("base")

	t.Run("no evidence: the move succeeds unstamped", func(t *testing.T) {
		id := r.story("Allocate", "ACME-SP-0001.R1", "ACME-SP-0001.R3")
		got := r.done(id)
		if len(got.SpecDelta.Stamped) != 0 || len(got.SpecDelta.Unstamped) != 2 ||
			got.SpecDelta.Unstamped[0].Reason != "no-results" || got.SpecDelta.Unstamped[1].Reason != "no-tests" {
			t.Errorf("done = %+v, want R1 no-results and R3 no-tests", got.SpecDelta)
		}
		if strings.Contains(r.read(covSpecPath), "verified:") {
			t.Errorf("a stamp was written without evidence:\n%s", r.read(covSpecPath))
		}
	})

	t.Run("only requirements whose linked tests all passed are stamped", func(t *testing.T) {
		r.record(c1, 1, map[string]Outcome{"TestNextID": OutcomePass, "TestFormat": OutcomeFail})
		id := r.story("Allocate and format", "ACME-SP-0001.R1", "ACME-SP-0001.R2")
		got := r.done(id)
		if len(got.SpecDelta.Stamped) != 1 || got.SpecDelta.Stamped[0].Ref != "ACME-SP-0001.R1" {
			t.Fatalf("stamped = %+v, want R1", got.SpecDelta.Stamped)
		}
		v := got.SpecDelta.Stamped[0].Verified
		if v.Commit != c1 || v.By != "ci" || !strings.HasPrefix(string(v.Rev), "sha256:") {
			t.Errorf("R1 stamp = %+v, want commit %s by ci", v, c1)
		}
		if len(got.SpecDelta.Unstamped) != 1 || got.SpecDelta.Unstamped[0].Ref != "ACME-SP-0001.R2" ||
			got.SpecDelta.Unstamped[0].Reason != "failed" {
			t.Errorf("unstamped = %+v, want R2 failed", got.SpecDelta.Unstamped)
		}
		spec := r.read(covSpecPath)
		if strings.Count(spec, "verified:") != 1 || !strings.Contains(spec, "commit: "+c1) {
			t.Errorf("spec after done:\n%s", spec)
		}
		var view struct {
			Requirement core.RequirementView `json:"requirement"`
		}
		if err := json.Unmarshal(r.call("requirement.get", map[string]any{"ref": "ACME-SP-0001.R1"}), &view); err != nil {
			t.Fatal(err)
		}
		if view.Requirement.Verified == nil || view.Requirement.Verified.Rev != view.Requirement.BlockRev {
			t.Errorf("R1 = %+v, want verified at its block rev", view.Requirement)
		}
		checkRow(t, r.coverage(), "ACME-SP-0001.R1", want{core.CoveragePassing, []string{"stamp"}})
	})

	t.Run("evidence without a recorded handle takes the assignee", func(t *testing.T) {
		entries := r.verifyEntries()
		for i := range entries {
			entries[i].By = ""
			entries[i].Result = core.VerifyPass
			entries[i].Tests = []core.VerifyTest{{Test: "src/alloc_test.go#TestFormat", Result: "pass"}}
		}
		data, err := core.EncodeVerifyCache(entries)
		if err != nil {
			t.Fatal(err)
		}
		r.write(verifyPath, string(data))
		id := r.story("Format", "ACME-SP-0001.R2")
		got := r.done(id)
		if len(got.SpecDelta.Stamped) != 1 || got.SpecDelta.Stamped[0].Verified.By != "marta" {
			t.Errorf("done = %+v, want R2 stamped by marta", got.SpecDelta)
		}
	})
}
