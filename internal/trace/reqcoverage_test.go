package trace

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/digiogithub/git-in-track/internal/core"
	"github.com/digiogithub/git-in-track/internal/core/osfs"
	"github.com/digiogithub/git-in-track/internal/gitops"
	"github.com/digiogithub/git-in-track/internal/vault"
)

// The coverage fixture: one spec with three requirements. R1 and R2 are
// implemented in src/alloc.go and verified in src/alloc_test.go; R3 has no
// test at all.
const (
	covProject = `schema: 2
key: ACME
name: Acme
workflow:
  statuses:
    - {id: todo, category: todo}
    - {id: done, category: done}
`
	covSpecPath = "docs/.pmngr/specs/ACME-SP-0001-allocation.md"
	covSpec     = `---
id: ACME-SP-0001
type: spec
title: Allocation
status: todo
created: 2026-01-01T00:00:00Z
updated: 2026-01-01T00:00:00Z
requirements:
  R1:
    status: todo
  R2:
    status: todo
  R3:
    status: todo
---

## Requirements

### ACME-SP-0001.R1 — Allocate by scan

The allocator SHALL allocate max + 1.

### ACME-SP-0001.R2 — Format

The allocator SHALL format ids.

### ACME-SP-0001.R3 — Report

The allocator SHALL report.
`
	covCode = `package alloc

// Implements: ACME-SP-0001.R1
func NextID() int {
	return 1
}

// Implements: ACME-SP-0001.R2
func Format() string {
	return "x"
}

func unrelated() int {
	return 0
}
`
	covTests = `package alloc

import "testing"

// Verifies: ACME-SP-0001.R1
func TestNextID(t *testing.T) {}

// Verifies: ACME-SP-0001.R2
func TestFormat(t *testing.T) {}
`
)

// covRepo is the fixture repository: a real git history, a vault over it
// with the trace engine and the coverage backend installed, and a
// test-result cache outside the working tree.
type covRepo struct {
	t     *testing.T
	root  string
	repo  *git.Repository
	vlt   *vault.Vault
	store *ResultStore
	base  time.Time
}

func newCovRepo(t *testing.T) *covRepo {
	t.Helper()
	root := t.TempDir()
	r := &covRepo{t: t, root: root, base: time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)}
	for p, text := range map[string]string{
		"docs/.pmngr/project.yaml": covProject,
		covSpecPath:                covSpec,
		"src/alloc.go":             covCode,
		"src/alloc_test.go":        covTests,
	} {
		r.write(p, text)
	}
	repo, err := git.PlainInit(root, false)
	if err != nil {
		t.Fatal(err)
	}
	r.repo = repo
	fsys, err := osfs.New(root)
	if err != nil {
		t.Fatal(err)
	}
	v, err := vault.New(vault.Options{FS: fsys, Root: "acme", Scan: true})
	if err != nil {
		t.Fatalf("open vault: %v", err)
	}
	backend, err := gitops.Open(root, gitops.Options{Backend: gitops.KindGoGit})
	if err != nil {
		t.Fatalf("open git: %v", err)
	}
	engine := NewEngine(root, EngineOptions{})
	r.store = NewResultStore(filepath.Join(t.TempDir(), "results.json"))
	v.SetRequirementTracer(engine)
	v.SetRequirementCoverage(NewCoverage(engine, ResultEvidence{Store: r.store}, GitChanges{Backend: backend}))
	r.vlt = v
	return r
}

func (r *covRepo) write(p, text string) {
	r.t.Helper()
	abs := filepath.Join(r.root, filepath.FromSlash(p))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(text), 0o644); err != nil {
		r.t.Fatal(err)
	}
}

func (r *covRepo) read(p string) string {
	r.t.Helper()
	data, err := os.ReadFile(filepath.Join(r.root, filepath.FromSlash(p)))
	if err != nil {
		r.t.Fatal(err)
	}
	return string(data)
}

// commit records the whole working tree and returns the commit id.
func (r *covRepo) commit(msg string) string {
	r.t.Helper()
	wt, err := r.repo.Worktree()
	if err != nil {
		r.t.Fatal(err)
	}
	if err := wt.AddGlob("."); err != nil {
		r.t.Fatal(err)
	}
	sig := &object.Signature{Name: "Test", Email: "test@example.com", When: r.base}
	h, err := wt.Commit(msg, &git.CommitOptions{Author: sig, Committer: sig})
	if err != nil {
		r.t.Fatal(err)
	}
	return h.String()
}

// ingest records results for the two tests at a commit, hours after base.
func (r *covRepo) ingest(commit string, hours int, outcomes map[string]Outcome) {
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
}

// clearResults deletes the test-result cache, as a fresh clone has none.
func (r *covRepo) clearResults() {
	r.t.Helper()
	if err := os.Remove(r.store.Path()); err != nil && !errors.Is(err, os.ErrNotExist) {
		r.t.Fatal(err)
	}
}

func (r *covRepo) call(method string, params any) json.RawMessage {
	r.t.Helper()
	data, err := json.Marshal(params)
	if err != nil {
		r.t.Fatal(err)
	}
	var env struct {
		OK     bool            `json:"ok"`
		Result json.RawMessage `json:"result"`
		Error  struct{ Code, Message string }
	}
	if err := json.Unmarshal([]byte(r.vlt.Call(method, string(data))), &env); err != nil {
		r.t.Fatal(err)
	}
	if !env.OK {
		r.t.Fatalf("%s: %s %s", method, env.Error.Code, env.Error.Message)
	}
	return env.Result
}

func (r *covRepo) coverage() map[string]core.CoverageRow {
	r.t.Helper()
	var got struct {
		Coverage []core.CoverageRow `json:"coverage"`
	}
	if err := json.Unmarshal(r.call("coverage.list", map[string]any{"spec": "ACME-SP-0001"}), &got); err != nil {
		r.t.Fatal(err)
	}
	out := map[string]core.CoverageRow{}
	for _, row := range got.Coverage {
		out[row.Ref.String()] = row
	}
	return out
}

type stampResult struct {
	Stamped []struct {
		Ref      string            `json:"ref"`
		Verified core.Verification `json:"verified"`
	} `json:"stamped"`
	Unstamped []struct {
		Ref    string `json:"ref"`
		Reason string `json:"reason"`
	} `json:"unstamped"`
}

func (r *covRepo) stamp(refs ...string) stampResult {
	r.t.Helper()
	var got stampResult
	if err := json.Unmarshal(r.call("requirement.stamp", map[string]any{"refs": refs, "by": "claude"}), &got); err != nil {
		r.t.Fatal(err)
	}
	return got
}

func (r *covRepo) editR1() {
	r.t.Helper()
	var got struct {
		Requirement core.RequirementView `json:"requirement"`
	}
	if err := json.Unmarshal(r.call("requirement.get", map[string]any{"ref": "ACME-SP-0001.R1"}), &got); err != nil {
		r.t.Fatal(err)
	}
	r.call("requirement.update", map[string]any{
		"ref": "ACME-SP-0001.R1", "rev": got.Requirement.Rev,
		"patch": map[string]any{"text": "The allocator SHALL allocate max + 1, never reusing an id."},
	})
}

// want is the expected coverage of one requirement: its status and reasons.
type want struct {
	status  core.CoverageStatus
	reasons []string
}

// TestCoverageTransitions walks one fixture repository through every status
// transition of docs/03 section 21.6, in order: untested, failing, passing
// from results, stamped, passing from the stamp, suspect by a block edit,
// untouched by an unrelated code change, suspect by a traced code change, and
// back to passing through newer results and a new stamp.
func TestCoverageTransitions(t *testing.T) {
	r := newCovRepo(t)
	c1 := r.commit("base")
	var c3 string

	steps := []struct {
		name string
		act  func(t *testing.T)
		want map[string]want
	}{
		{"no results", nil, map[string]want{
			"ACME-SP-0001.R1": {core.CoverageUntested, []string{"no-results"}},
			"ACME-SP-0001.R2": {core.CoverageUntested, []string{"no-results"}},
			"ACME-SP-0001.R3": {core.CoverageUntested, []string{"no-tests"}},
		}},
		{"failing and passing results", func(t *testing.T) {
			r.ingest(c1, 1, map[string]Outcome{"TestNextID": OutcomeFail, "TestFormat": OutcomePass})
			got := r.stamp("ACME-SP-0001.R1")
			if len(got.Stamped) != 0 || len(got.Unstamped) != 1 || got.Unstamped[0].Reason != "failed" {
				t.Errorf("stamping a failing requirement = %+v, want unstamped: failed", got)
			}
		}, map[string]want{
			"ACME-SP-0001.R1": {core.CoverageFailing, []string{"failed", "results"}},
			"ACME-SP-0001.R2": {core.CoveragePassing, []string{"results"}},
			"ACME-SP-0001.R3": {core.CoverageUntested, []string{"no-tests"}},
		}},
		{"fixed", func(*testing.T) {
			r.ingest(c1, 2, map[string]Outcome{"TestNextID": OutcomePass})
		}, map[string]want{
			"ACME-SP-0001.R1": {core.CoveragePassing, []string{"results"}},
			"ACME-SP-0001.R2": {core.CoveragePassing, []string{"results"}},
		}},
		{"stamped, cache deleted", func(t *testing.T) {
			got := r.stamp("ACME-SP-0001.R1", "ACME-SP-0001.R2", "ACME-SP-0001.R3")
			if len(got.Stamped) != 2 || len(got.Unstamped) != 1 || got.Unstamped[0].Reason != "no-tests" {
				t.Fatalf("stamp = %+v, want R1 and R2 stamped, R3 unstamped: no-tests", got)
			}
			for _, s := range got.Stamped {
				if s.Verified.Commit != c1 || s.Verified.By != "claude" || !strings.HasPrefix(string(s.Verified.Rev), "sha256:") {
					t.Errorf("stamp of %s = %+v", s.Ref, s.Verified)
				}
			}
			spec := r.read(covSpecPath)
			if strings.Count(spec, "verified:") != 2 || !strings.Contains(spec, "commit: "+c1) {
				t.Errorf("spec after stamping:\n%s", spec)
			}
			if again := r.stamp("ACME-SP-0001.R1"); len(again.Unstamped) != 1 || again.Unstamped[0].Reason != "unchanged" {
				t.Errorf("restamp = %+v, want unchanged", again)
			}
			r.clearResults()
		}, map[string]want{
			"ACME-SP-0001.R1": {core.CoveragePassing, []string{"stamp"}},
			"ACME-SP-0001.R2": {core.CoveragePassing, []string{"stamp"}},
			"ACME-SP-0001.R3": {core.CoverageUntested, []string{"no-tests"}},
		}},
		{"block edited", func(*testing.T) { r.editR1() }, map[string]want{
			"ACME-SP-0001.R1": {core.CoverageSuspect, []string{"text", "stamp"}},
			"ACME-SP-0001.R2": {core.CoveragePassing, []string{"stamp"}},
		}},
		{"unrelated code changed", func(*testing.T) {
			r.write("src/alloc.go", strings.Replace(covCode, "return 0", "return 42", 1))
			r.commit("unrelated")
		}, map[string]want{
			"ACME-SP-0001.R2": {core.CoveragePassing, []string{"stamp"}},
		}},
		{"traced code changed", func(*testing.T) {
			r.write("src/alloc.go", strings.Replace(strings.Replace(covCode, "return 0", "return 42", 1), `return "x"`, `return "y"`, 1))
			c3 = r.commit("format")
		}, map[string]want{
			"ACME-SP-0001.R1": {core.CoverageSuspect, []string{"text", "stamp"}},
			"ACME-SP-0001.R2": {core.CoverageSuspect, []string{"code:src/alloc.go#Format", "stamp"}},
		}},
		{"newer results", func(*testing.T) {
			r.ingest(c3, 5, map[string]Outcome{"TestNextID": OutcomePass, "TestFormat": OutcomePass})
		}, map[string]want{
			// The test-result cache does not record which text ran, so it
			// cannot clear a text change: only a new stamp does.
			"ACME-SP-0001.R1": {core.CoverageSuspect, []string{"text", "stamp"}},
			"ACME-SP-0001.R2": {core.CoveragePassing, []string{"results"}},
		}},
		{"restamped", func(t *testing.T) {
			got := r.stamp("ACME-SP-0001.R1", "ACME-SP-0001.R2")
			if len(got.Stamped) != 2 {
				t.Fatalf("restamp = %+v", got)
			}
			r.clearResults()
		}, map[string]want{
			"ACME-SP-0001.R1": {core.CoveragePassing, []string{"stamp"}},
			"ACME-SP-0001.R2": {core.CoveragePassing, []string{"stamp"}},
		}},
		{"failing after the stamp", func(*testing.T) {
			r.ingest(c3, 9, map[string]Outcome{"TestNextID": OutcomeFail, "TestFormat": OutcomePass})
		}, map[string]want{
			"ACME-SP-0001.R1": {core.CoverageFailing, []string{"failed", "results"}},
			"ACME-SP-0001.R2": {core.CoveragePassing, []string{"results"}},
		}},
	}
	for _, step := range steps {
		if step.act != nil {
			step.act(t)
		}
		rows := r.coverage()
		for ref, w := range step.want {
			row, ok := rows[ref]
			if !ok {
				t.Fatalf("%s: no row for %s", step.name, ref)
			}
			if row.Status != w.status || !reflect.DeepEqual(row.Reasons, w.reasons) {
				t.Errorf("%s: %s = %s %v, want %s %v", step.name, ref, row.Status, row.Reasons, w.status, w.reasons)
			}
		}
	}
	for _, key := range []string{"suspect", "coverage", "tested"} {
		if strings.Contains(r.read(covSpecPath), key+":") {
			t.Errorf("the spec stores a %q key", key)
		}
	}
}

// TestCoverageRowShape pins the compact row an agent reads.
func TestCoverageRowShape(t *testing.T) {
	t.Parallel()
	r := newCovRepo(t)
	c1 := r.commit("base")
	r.ingest(c1, 1, map[string]Outcome{"TestNextID": OutcomePass})
	var got struct {
		Coverage []json.RawMessage `json:"coverage"`
	}
	if err := json.Unmarshal(r.call("coverage.list", map[string]any{"refs": []string{"ACME-SP-0001.R1"}}), &got); err != nil {
		t.Fatal(err)
	}
	want := `{"ref":"ACME-SP-0001.R1","status":"passing","reasons":["results"],"tests":[{"test":"src/alloc_test.go#TestNextID","result":"pass"}]}`
	if len(got.Coverage) != 1 || string(got.Coverage[0]) != want {
		t.Errorf("row = %s\nwant  %s", got.Coverage, want)
	}
}

// TestClassify covers the rules the fixture history does not reach.
func TestClassify(t *testing.T) {
	t.Parallel()
	ref := core.RequirementRef{Spec: "ACME-SP-0001", Number: 1}
	t0 := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	stamp := &core.Verification{Rev: "sha256:a", Commit: "c1", At: core.NewTimestamp(t0)}
	noDrift := func(string) ([]string, error) { return nil, nil }
	tests := []struct {
		name  string
		in    CoverageInput
		drift DriftFunc
		want  want
	}{
		{"partial without a stamp", CoverageInput{Linked: 2, Evidence: Evidence{Result: RequirementPartial}},
			noDrift, want{core.CoverageUntested, []string{"partial"}}},
		{"partial after a stamp", CoverageInput{BlockRev: "sha256:a", Stamp: stamp, Linked: 2,
			Evidence: Evidence{Result: RequirementPartial, At: t0.Add(time.Hour)}},
			noDrift, want{core.CoveragePassing, []string{"stamp", "partial"}}},
		{"older failing results lose to the stamp", CoverageInput{BlockRev: "sha256:a", Stamp: stamp, Linked: 1,
			Evidence: Evidence{Result: RequirementFail, At: t0.Add(-time.Hour)}},
			noDrift, want{core.CoveragePassing, []string{"stamp"}}},
		{"a run of the current text clears a text change", CoverageInput{BlockRev: "sha256:b", Stamp: stamp, Linked: 1,
			Evidence: Evidence{Result: RequirementPass, Rev: "sha256:b", Commits: []string{"c2"}, At: t0.Add(time.Hour)}},
			noDrift, want{core.CoveragePassing, []string{"results"}}},
		{"a run of another text is not evidence", CoverageInput{BlockRev: "sha256:a", Linked: 1,
			Evidence: Evidence{Result: RequirementFail, Rev: "sha256:z", At: t0}},
			noDrift, want{core.CoverageUntested, []string{"no-results"}}},
		{"hand-written stamp without a commit", CoverageInput{BlockRev: "sha256:a", Stamp: &core.Verification{Rev: "sha256:a"}},
			noDrift, want{core.CoveragePassing, []string{"unchecked", "stamp"}}},
		{"no history", CoverageInput{BlockRev: "sha256:a", Stamp: stamp},
			nil, want{core.CoveragePassing, []string{"unchecked", "stamp"}}},
		{"commit not in the history", CoverageInput{BlockRev: "sha256:a", Stamp: stamp},
			func(string) ([]string, error) { return nil, ErrUnknownCommit },
			want{core.CoverageSuspect, []string{"commit-unknown", "stamp"}}},
		{"drift is capped", CoverageInput{BlockRev: "sha256:a", Stamp: stamp},
			func(string) ([]string, error) {
				return []string{"code:e.go", "code:d.go", "test:a_test.go#T", "code:c.go#F", "code:b.go"}, nil
			},
			want{core.CoverageSuspect, []string{"code:b.go", "code:c.go#F", "code:d.go", "+2", "stamp"}}},
		{"drift since every commit of the results", CoverageInput{Linked: 1,
			Evidence: Evidence{Result: RequirementPass, Commits: []string{"c1", "c2"}, At: t0}},
			func(c string) ([]string, error) {
				if c == "c1" {
					return []string{"code:a.go#F"}, nil
				}
				return nil, nil
			},
			want{core.CoverageSuspect, []string{"code:a.go#F", "results"}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tc.in.Ref = ref
			got, err := Classify(tc.in, tc.drift)
			if err != nil {
				t.Fatal(err)
			}
			if got.Status != tc.want.status || !reflect.DeepEqual(got.Reasons, tc.want.reasons) {
				t.Errorf("Classify = %s %v, want %s %v", got.Status, got.Reasons, tc.want.status, tc.want.reasons)
			}
		})
	}
}

// TestStampEvidence covers the refusals a stamp writer is handed.
func TestStampEvidence(t *testing.T) {
	t.Parallel()
	r := newCovRepo(t)
	c1 := r.commit("base")
	r.ingest(c1, 1, map[string]Outcome{"TestNextID": OutcomePass})
	r.ingest(c1, 3, map[string]Outcome{"TestFormat": OutcomePass})
	got := r.stamp("ACME-SP-0001.R1", "ACME-SP-0001.R2")
	if len(got.Stamped) != 2 {
		t.Fatalf("stamp = %+v", got)
	}
	if at := got.Stamped[0].Verified.At.Time; !at.Equal(r.base.Add(time.Hour)) {
		t.Errorf("stamp at = %s, want the time of the passing run", at)
	}

	// A stamp is never replaced by an older run.
	r2 := newCovRepo(t)
	d1 := r2.commit("base")
	r2.ingest(d1, 5, map[string]Outcome{"TestNextID": OutcomePass})
	r2.stamp("ACME-SP-0001.R1")
	r2.clearResults()
	r2.ingest("0000000000000000000000000000000000000000", 1, map[string]Outcome{"TestNextID": OutcomePass})
	if again := r2.stamp("ACME-SP-0001.R1"); len(again.Unstamped) != 1 || again.Unstamped[0].Reason != "stamp-newer" {
		t.Errorf("stamping an older run = %+v, want stamp-newer", again)
	}
}

// TestCoverageWithoutChanges checks a repository without history: drift is
// left unchecked rather than guessed.
func TestCoverageWithoutChanges(t *testing.T) {
	t.Parallel()
	c := NewCoverage(NewEngine(t.TempDir(), EngineOptions{}), nil, nil)
	if c.changes != nil {
		t.Fatal("a nil change lister must stay nil")
	}
	if _, ok := c.evidence.(ResultEvidence); !ok {
		t.Fatal("a nil evidence source is an empty test-result cache")
	}
}
