package impact

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/digiogithub/git-in-track/internal/core"
	"github.com/digiogithub/git-in-track/internal/core/osfs"
	"github.com/digiogithub/git-in-track/internal/gitops"
	"github.com/digiogithub/git-in-track/internal/pando"
	"github.com/digiogithub/git-in-track/internal/trace"
	"github.com/digiogithub/git-in-track/internal/vault"
)

var update = flag.Bool("update", false, "rewrite the golden files under testdata/")

// The fixture: one spec with four requirements.
//
//   - R1 is implemented by NextID in src/alloc.go and verified by TestNextID.
//   - R2 is implemented by Format in src/format.go, which calls NextID, and
//     verified by TestFormat: a change of NextID reaches it transitively.
//   - R3 is implemented by Report in src/report.go, which nothing changed
//     calls: only the semantic tier can offer it.
//   - R4 has no code; the open story ACME-US-0001 modifies it in its Spec
//     Delta, a pending modifies edge.
const (
	fxProject = `schema: 2
key: ACME
name: Acme
workflow:
  statuses:
    - {id: todo, category: todo}
    - {id: in_progress, category: in_progress}
    - {id: done, category: done}
`
	fxSpecPath = "docs/.pmngr/specs/ACME-SP-0001-allocation.md"
	fxSpec     = `---
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
  R4:
    status: todo
---

## Requirements

### ACME-SP-0001.R1 — Allocate by scan

The allocator SHALL allocate max + 1.

### ACME-SP-0001.R2 — Format ids

The allocator SHALL format ids with four digits.

### ACME-SP-0001.R3 — Report allocations

The allocator SHALL report every allocation.

### ACME-SP-0001.R4 — Reserve numbers

The allocator SHALL reserve the numbers a delta names.
`
	fxStoryPath = "docs/.pmngr/stories/ACME-US-0001-reserve-delta-numbers.md"
	fxStory     = `---
id: ACME-US-0001
type: story
title: Reserve the numbers a delta names
status: in_progress
created: 2026-01-01T00:00:00Z
updated: 2026-01-01T00:00:00Z
---

## Spec Delta

### MODIFIED ACME-SP-0001.R4 — Reserve numbers

The allocator SHALL reserve every number a delta names, applied or not.
`
	fxAlloc = `package alloc

// Implements: ACME-SP-0001.R1
func NextID(ids []int) int {
	max := 0
	for _, id := range ids {
		if id > max {
			max = id
		}
	}
	return max + 1
}
`
	fxFormat = `package alloc

import "fmt"

// Implements: ACME-SP-0001.R2
func Format(ids []int) string {
	return fmt.Sprintf("%04d", NextID(ids))
}
`
	fxReport = `package alloc

// Implements: ACME-SP-0001.R3
func Report() string {
	return "report"
}
`
	fxTests = `package alloc

import "testing"

// Verifies: ACME-SP-0001.R1
func TestNextID(t *testing.T) {}

// Verifies: ACME-SP-0001.R2
func TestFormat(t *testing.T) {}
`
)

// fixture is a real git repository with a vault, the trace engine, the
// coverage backend over a test-result cache, and an impact resolver.
type fixture struct {
	t        *testing.T
	root     string
	repo     *git.Repository
	vlt      *vault.Vault
	engine   *trace.Engine
	coverage *trace.Coverage
	backend  gitops.Backend
	store    *trace.ResultStore
	base     string // the commit the results were recorded at
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	root := t.TempDir()
	f := &fixture{t: t, root: root}
	for p, text := range map[string]string{
		"docs/.pmngr/project.yaml": fxProject,
		fxSpecPath:                 fxSpec,
		fxStoryPath:                fxStory,
		"src/alloc.go":             fxAlloc,
		"src/format.go":            fxFormat,
		"src/report.go":            fxReport,
		"src/alloc_test.go":        fxTests,
	} {
		f.write(p, text)
	}
	repo, err := git.PlainInit(root, false)
	if err != nil {
		t.Fatal(err)
	}
	f.repo = repo
	f.base = f.commit("initial")

	fsys, err := osfs.New(root)
	if err != nil {
		t.Fatal(err)
	}
	v, err := vault.New(vault.Options{FS: fsys, Root: "acme", Scan: true})
	if err != nil {
		t.Fatalf("open vault: %v", err)
	}
	f.vlt = v
	if f.backend, err = gitops.Open(root, gitops.Options{Backend: gitops.KindGoGit}); err != nil {
		t.Fatalf("open git: %v", err)
	}
	f.engine = trace.NewEngine(root, trace.EngineOptions{})
	f.store = trace.NewResultStore(filepath.Join(t.TempDir(), "results.json"))
	f.coverage = trace.NewCoverage(f.engine, trace.ResultEvidence{Store: f.store}, trace.GitChanges{Backend: f.backend})
	v.SetRequirementTracer(f.engine)
	v.SetRequirementCoverage(f.coverage)

	// Both tests passed at the initial commit.
	at := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	var results []trace.TestResult
	for _, symbol := range []string{"TestNextID", "TestFormat"} {
		results = append(results, trace.TestResult{
			ID: "example.com/acme/src#" + symbol, Format: trace.FormatGoTest, Path: "src/alloc_test.go",
			Symbol: symbol, Result: trace.OutcomePass, Commit: f.base, At: at,
		})
	}
	if _, err := f.store.Merge(root, results); err != nil {
		t.Fatal(err)
	}

	// The diff under test: NextID's body changes in the working tree.
	f.write("src/alloc.go", strings.Replace(fxAlloc, "return max + 1", "return max + 2", 1))
	return f
}

func (f *fixture) write(p, text string) {
	f.t.Helper()
	abs := filepath.Join(f.root, filepath.FromSlash(p))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(text), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) commit(msg string) string {
	f.t.Helper()
	wt, err := f.repo.Worktree()
	if err != nil {
		f.t.Fatal(err)
	}
	if err := wt.AddGlob("."); err != nil {
		f.t.Fatal(err)
	}
	sig := &object.Signature{Name: "Test", Email: "test@example.com", When: time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)}
	h, err := wt.Commit(msg, &git.CommitOptions{Author: sig, Committer: sig})
	if err != nil {
		f.t.Fatal(err)
	}
	return h.String()
}

// resolver builds a resolver over the fixture with the given Pando fakes;
// nil fakes are an absent Pando.
func (f *fixture) resolver(g CallGraph, s vault.SemanticSearcher) *Resolver {
	opts := Options{Engine: f.engine, Differ: f.backend, Coverage: f.coverage, ProjectID: "acme"}
	if g != nil {
		opts.CallGraph = func() CallGraph { return g }
	}
	if s != nil {
		opts.Semantic = func() vault.SemanticSearcher { return s }
	}
	return New(opts)
}

func (f *fixture) impact(r *Resolver, q core.ImpactQuery) core.ImpactResult {
	f.t.Helper()
	f.vlt.SetRequirementImpact(r)
	params, err := json.Marshal(q)
	if err != nil {
		f.t.Fatal(err)
	}
	var env struct {
		OK     bool `json:"ok"`
		Result struct {
			Impact core.ImpactResult `json:"impact"`
		} `json:"result"`
		Error struct{ Code, Message string } `json:"error"`
	}
	if err := json.Unmarshal([]byte(f.vlt.Call("impact.query", string(params))), &env); err != nil {
		f.t.Fatal(err)
	}
	if !env.OK {
		f.t.Fatalf("impact.query: %s %s", env.Error.Code, env.Error.Message)
	}
	return env.Result.Impact
}

// lineOf returns the 1-based line of the first line of text that contains s.
func lineOf(text, s string) int {
	for i, l := range strings.Split(text, "\n") {
		if strings.Contains(l, s) {
			return i + 1
		}
	}
	return 0
}

// fakeGraph answers code_impact_analysis from a fixed caller table.
type fakeGraph struct {
	callers map[string][]pando.ImpactCaller
	err     error
	asked   []string
}

func (g *fakeGraph) ImpactAnalysis(_ context.Context, projectID string, symbols []string, o pando.ImpactOptions) (pando.ImpactResult, error) {
	if g.err != nil {
		return pando.ImpactResult{}, g.err
	}
	if projectID != "acme" || o.Depth != DefaultDepth || o.Limit != DefaultLimit || len(symbols) != 1 {
		return pando.ImpactResult{}, fmt.Errorf("unexpected call: %q %v %+v", projectID, symbols, o)
	}
	g.asked = append(g.asked, symbols[0])
	var out pando.ImpactResult
	for _, c := range g.callers[symbols[0]] {
		c.Symbol = symbols[0]
		out.Callers = append(out.Callers, c)
	}
	return out, nil
}

// fixtureGraph says Format calls NextID, and that a file outside the
// repository does too (ignored).
func fixtureGraph() *fakeGraph {
	return &fakeGraph{callers: map[string][]pando.ImpactCaller{
		"NextID": {
			{Name: "Format", NamePath: "/Format", FilePath: "src/format.go", StartLine: lineOf(fxFormat, "func Format"), Depth: 1},
			{Name: "Other", FilePath: "../elsewhere/x.go", StartLine: 3, Depth: 2},
		},
	}}
}

// fakeSemantic answers a fixed ranking of requirement blocks.
type fakeSemantic struct {
	hits  []core.SearchHit
	err   error
	query vault.SemanticQuery
}

func (s *fakeSemantic) SearchSemantic(_ context.Context, q vault.SemanticQuery) ([]core.SearchHit, error) {
	s.query = q
	return s.hits, s.err
}

func fixtureSemantic() *fakeSemantic {
	return &fakeSemantic{hits: []core.SearchHit{
		{Kind: core.SearchKindRequirement, ID: "ACME-SP-0001.R1", Score: 1},
		{Kind: core.SearchKindRequirement, ID: "ACME-SP-0001.R3", Score: 0.61234},
		{Kind: core.SearchKindRequirement, ID: "OTHER-SP-0001.R1", Score: 0.5},
		{Kind: "item", ID: "ACME-SP-0001", Score: 0.4},
	}}
}

// TestImpactGolden pins tiers 1 and 2: the same diff and index give a
// byte-identical answer.
func TestImpactGolden(t *testing.T) {
	f := newFixture(t)
	q := core.ImpactQuery{Base: f.base, Story: "ACME-US-0001", Tiers: []int{1, 2}}
	first := f.impact(f.resolver(fixtureGraph(), nil), q)
	second := f.impact(f.resolver(fixtureGraph(), nil), q)
	a, err := json.MarshalIndent(first, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.MarshalIndent(second, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Fatalf("two runs differ:\n%s\n%s", a, b)
	}
	// The base commit id is the only varying part across machines; pin it.
	checkGolden(t, "impact_tiers12.golden.json", bytes.ReplaceAll(append(a, '\n'), []byte(f.base), []byte("<base>")))
}

// TestImpactGoldenReverified pins the answer once the diff is committed and
// the linked tests re-ran and were ingested at its head (GIT-US-0148): the
// touched requirements are passing and not suspect.
func TestImpactGoldenReverified(t *testing.T) {
	f := newFixture(t)
	head := f.commit("change NextID")
	f.ingestAt(head, map[string]trace.Outcome{"TestNextID": trace.OutcomePass, "TestFormat": trace.OutcomePass})
	res := f.impact(f.resolver(fixtureGraph(), nil), core.ImpactQuery{Base: f.base, Story: "ACME-US-0001", Tiers: []int{1, 2}})
	a, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got := bytes.ReplaceAll(append(a, '\n'), []byte(f.base), []byte("<base>"))
	checkGolden(t, "impact_reverified.golden.json", bytes.ReplaceAll(got, []byte(head), []byte("<head>")))
}

// checkGolden compares got with testdata/name, rewriting it under -update.
func checkGolden(t *testing.T, name string, got []byte) {
	t.Helper()
	golden := filepath.Join("testdata", name)
	if *update {
		if err := os.WriteFile(golden, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read golden (run with -update to create it): %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("impact differs from %s:\n%s", golden, got)
	}
}

func TestImpactTiers(t *testing.T) {
	f := newFixture(t)
	byRef := func(res core.ImpactResult) map[string]core.ImpactHit {
		out := map[string]core.ImpactHit{}
		for _, h := range res.Hits {
			out[h.Ref.String()] = h
		}
		return out
	}

	t.Run("all three tiers", func(t *testing.T) {
		graph, sem := fixtureGraph(), fixtureSemantic()
		res := f.impact(f.resolver(graph, sem), core.ImpactQuery{Base: f.base, Story: "ACME-US-0001"})
		hits := byRef(res)
		if res.Files != 1 || res.Symbols != 1 {
			t.Errorf("files, symbols = %d, %d, want 1, 1", res.Files, res.Symbols)
		}
		for i, want := range []core.ImpactTierStatus{core.ImpactTierOK, core.ImpactTierOK, core.ImpactTierOK} {
			if res.Tiers[i].Status != want {
				t.Errorf("tier %d = %+v, want %s", i+1, res.Tiers[i], want)
			}
		}
		r1, r2, r3, r4 := hits["ACME-SP-0001.R1"], hits["ACME-SP-0001.R2"], hits["ACME-SP-0001.R3"], hits["ACME-SP-0001.R4"]
		if r1.Tier != 1 || r1.Candidate || r1.Status != core.CoverageSuspect || !r1.Suspect {
			t.Errorf("R1 = %+v, want a suspect tier-1 hit, not a candidate", r1)
		}
		if r2.Tier != 2 || r2.Status != core.CoveragePassing || !r2.Suspect ||
			len(r2.Reasons) != 1 || r2.Reasons[0] != "call:src/format.go#Format calls NextID d1" {
			t.Errorf("R2 = %+v, want a passing tier-2 hit made suspect by the call", r2)
		}
		if r3.Tier != 3 || !r3.Candidate || r3.Score != 0.612 || r3.Suspect || r3.Reasons[0] != "semantic" {
			t.Errorf("R3 = %+v, want a tier-3 candidate scored 0.612", r3)
		}
		if r4.Tier != 1 || r4.Reasons[0] != "delta:ACME-US-0001" || len(r4.Pending) != 1 || r4.Suspect {
			t.Errorf("R4 = %+v, want a tier-1 delta hit with a pending edge", r4)
		}
		if len(res.Hits) != 4 {
			t.Errorf("hits = %+v, want four (no foreign or item candidate)", res.Hits)
		}
		if sem.query.Kind != core.SearchKindRequirement ||
			sem.query.Q != "Reserve the numbers a delta names NextID" {
			t.Errorf("semantic query = %+v", sem.query)
		}
		if strings.Join(graph.asked, ",") != "NextID" {
			t.Errorf("Pando asked about %v, want NextID only", graph.asked)
		}
		data, err := json.Marshal(res)
		if err != nil {
			t.Fatal(err)
		}
		// A rough token estimate (bytes / 4) of the report, for the budget of
		// GIT-US-0120: a typical PR must stay under 1.5k tokens.
		t.Logf("impact report: %d bytes, about %d tokens", len(data), len(data)/4)
		if len(data)/4 > 1500 {
			t.Errorf("the fixture report is %d bytes, over the 1.5k-token budget", len(data))
		}
	})

	t.Run("without pando tier 1 still answers", func(t *testing.T) {
		res := f.impact(f.resolver(nil, nil), core.ImpactQuery{Base: f.base})
		if res.Tiers[0].Status != core.ImpactTierOK ||
			res.Tiers[1].Status != core.ImpactTierUnavailable || res.Tiers[2].Status != core.ImpactTierUnavailable {
			t.Errorf("tiers = %+v, want ok, unavailable, unavailable", res.Tiers)
		}
		if len(res.Hits) != 1 || res.Hits[0].Ref.String() != "ACME-SP-0001.R1" ||
			res.Hits[0].Reasons[0] != "symbol:src/alloc.go#NextID" {
			t.Errorf("hits = %+v, want R1 by its symbol", res.Hits)
		}
	})

	t.Run("pando unreachable or failing", func(t *testing.T) {
		for _, tc := range []struct {
			err  error
			want core.ImpactTierStatus
		}{
			{pando.ErrUnreachable, core.ImpactTierUnavailable},
			{fmt.Errorf("wrapped: %w", pando.ErrTimeout), core.ImpactTierUnavailable},
			{pando.ErrToolFailed, core.ImpactTierError},
		} {
			res := f.impact(f.resolver(&fakeGraph{err: tc.err}, &fakeSemantic{err: tc.err}), core.ImpactQuery{Base: f.base})
			if res.Tiers[1].Status != tc.want || res.Tiers[2].Status != tc.want || res.Tiers[1].Message == "" {
				t.Errorf("%v: tiers = %+v, want %s", tc.err, res.Tiers, tc.want)
			}
			if len(res.Hits) != 1 {
				t.Errorf("%v: hits = %+v, want tier 1 only", tc.err, res.Hits)
			}
		}
	})

	t.Run("tiers the caller skipped", func(t *testing.T) {
		graph := fixtureGraph()
		res := f.impact(f.resolver(graph, fixtureSemantic()), core.ImpactQuery{Base: f.base, Tiers: []int{3}})
		if res.Tiers[0].Status != core.ImpactTierSkipped || res.Tiers[1].Status != core.ImpactTierSkipped {
			t.Errorf("tiers = %+v, want 1 and 2 skipped", res.Tiers)
		}
		if len(graph.asked) != 0 {
			t.Errorf("Pando asked %v for a skipped tier", graph.asked)
		}
		got := byRef(res)
		if len(res.Hits) != 2 || !got["ACME-SP-0001.R1"].Candidate || !got["ACME-SP-0001.R3"].Candidate {
			t.Errorf("hits = %+v, want R1 and R3 as candidates", res.Hits)
		}
	})

	t.Run("clean diff", func(t *testing.T) {
		res := f.impact(f.resolver(fixtureGraph(), nil), core.ImpactQuery{Base: "HEAD", Head: "HEAD"})
		if res.Files != 0 || len(res.Hits) != 0 {
			t.Errorf("HEAD..HEAD = %+v, want nothing", res)
		}
	})
}

func TestImpactErrors(t *testing.T) {
	f := newFixture(t)
	f.vlt.SetRequirementImpact(f.resolver(nil, nil))
	for _, tc := range []struct {
		name, params, code string
	}{
		{"unknown revision", `{"base":"no-such-branch"}`, "invalid_request"},
		{"unknown tier", `{"tiers":[4]}`, "invalid_request"},
		{"depth out of range", `{"depth":99}`, "invalid_request"},
		{"unknown story", `{"story":"ACME-US-0999"}`, "not_found"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var env struct {
				OK    bool                           `json:"ok"`
				Error struct{ Code, Message string } `json:"error"`
			}
			if err := json.Unmarshal([]byte(f.vlt.Call("impact.query", tc.params)), &env); err != nil {
				t.Fatal(err)
			}
			if env.OK || env.Error.Code != tc.code {
				t.Errorf("impact.query %s = %+v, want %s", tc.params, env, tc.code)
			}
		})
	}
	if _, err := New(Options{}).Impact(context.Background(), nil, core.ImpactQuery{}); err == nil {
		t.Error("a resolver without an engine answered")
	}
}

func TestPandoName(t *testing.T) {
	for in, want := range map[string]string{
		"NextID":                "NextID",
		"Store.Load":            "Load",
		"TestX/sub_case":        "TestX",
		"describe > it renders": "",
	} {
		if got := pandoName(in); got != want {
			t.Errorf("pandoName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRepoPath(t *testing.T) {
	for in, want := range map[string]string{
		"src/a.go":    "src/a.go",
		"./src/a.go":  "src/a.go",
		"src\\a.go":   "src/a.go",
		"/abs/a.go":   "",
		"../out/a.go": "",
		"":            "",
	} {
		got, ok := repoPath(in)
		if (want == "") == ok || got != want {
			t.Errorf("repoPath(%q) = %q, %v, want %q", in, got, ok, want)
		}
	}
}

// TestNotImportedByCoreOrVault keeps the resolver out of the WASM build: the
// packages compiled to WebAssembly reach it only through the vault seam.
func TestNotImportedByCoreOrVault(t *testing.T) {
	t.Parallel()
	const self = "github.com/digiogithub/git-in-track/internal/impact"
	for _, dir := range []string{"../core", "../vault", "../../wasm"} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
				continue
			}
			f, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, e.Name()), nil, parser.ImportsOnly)
			if err != nil {
				t.Fatal(err)
			}
			for _, imp := range f.Imports {
				if strings.Trim(imp.Path.Value, `"`) == self {
					t.Errorf("%s/%s imports %s", dir, e.Name(), self)
				}
			}
		}
	}
}

// ingestAt records the given outcomes of the fixture's two tests at commit,
// a day after the initial results, as `gintrack spec ingest` would.
func (f *fixture) ingestAt(commit string, outcomes map[string]trace.Outcome) {
	f.t.Helper()
	at := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	var results []trace.TestResult
	for symbol, o := range outcomes {
		results = append(results, trace.TestResult{
			ID: "example.com/acme/src#" + symbol, Format: trace.FormatGoTest, Path: "src/alloc_test.go",
			Symbol: symbol, Result: o, Commit: commit, At: at,
		})
	}
	if _, err := f.store.Merge(f.root, results); err != nil {
		f.t.Fatal(err)
	}
}

// TestImpactSuspectAtHead pins when a passing requirement the diff touches
// is suspect (GIT-US-0148, docs/03 R-IMP-5): unless its linked tests all
// passed in results ingested at the diff's head commit, or its stamp names
// that commit on the current text. Failing wins; a pending Spec Delta alone
// clears nothing.
func TestImpactSuspectAtHead(t *testing.T) {
	both := map[string]trace.Outcome{"TestNextID": trace.OutcomePass, "TestFormat": trace.OutcomePass}
	type state struct {
		status  core.CoverageStatus
		suspect bool
	}
	for _, tc := range []struct {
		name string
		// setup commits (or not) the diff and ingests; it returns the head
		// of the query, "" for the working tree.
		setup  func(t *testing.T, f *fixture) string
		r1, r2 state
	}{
		{"uncommitted diff, results at HEAD: a dirty tree has no head commit", func(t *testing.T, f *fixture) string {
			return ""
		}, state{core.CoverageSuspect, true}, state{core.CoveragePassing, true}},
		{"committed diff, tests not re-run", func(t *testing.T, f *fixture) string {
			f.commit("change NextID")
			return ""
		}, state{core.CoverageSuspect, true}, state{core.CoveragePassing, true}},
		{"committed diff, tests re-run and ingested at head", func(t *testing.T, f *fixture) string {
			f.ingestAt(f.commit("change NextID"), both)
			return ""
		}, state{core.CoveragePassing, false}, state{core.CoveragePassing, false}},
		{"head named as a revision", func(t *testing.T, f *fixture) string {
			head := f.commit("change NextID")
			f.ingestAt(head, both)
			return head
		}, state{core.CoveragePassing, false}, state{core.CoveragePassing, false}},
		{"head named as a revision the results were not taken at", func(t *testing.T, f *fixture) string {
			head := f.commit("change NextID")
			f.write("src/report.go", strings.Replace(fxReport, `"report"`, `"reports"`, 1))
			f.ingestAt(f.commit("change Report"), both)
			return head
		}, state{core.CoveragePassing, true}, state{core.CoveragePassing, true}},
		{"re-run at head, a linked test fails", func(t *testing.T, f *fixture) string {
			f.ingestAt(f.commit("change NextID"), map[string]trace.Outcome{"TestNextID": trace.OutcomeFail, "TestFormat": trace.OutcomePass})
			return ""
		}, state{core.CoverageFailing, false}, state{core.CoveragePassing, false}},
		{"only one requirement's tests re-run at head", func(t *testing.T, f *fixture) string {
			f.ingestAt(f.commit("change NextID"), map[string]trace.Outcome{"TestNextID": trace.OutcomePass})
			return ""
		}, state{core.CoveragePassing, false}, state{core.CoveragePassing, true}},
		{"re-run at head, results without a commit", func(t *testing.T, f *fixture) string {
			f.commit("change NextID")
			f.ingestAt("", both)
			return ""
		}, state{core.CoveragePassing, true}, state{core.CoveragePassing, true}},
		{"re-run at head, an uncommitted backlog edit", func(t *testing.T, f *fixture) string {
			f.ingestAt(f.commit("change NextID"), both)
			f.write(fxStoryPath, strings.Replace(fxStory, "status: in_progress", "status: done", 1))
			return ""
		}, state{core.CoveragePassing, false}, state{core.CoveragePassing, false}},
		{"re-run at head, then an uncommitted code edit", func(t *testing.T, f *fixture) string {
			f.ingestAt(f.commit("change NextID"), both)
			f.write("src/report.go", strings.Replace(fxReport, `"report"`, `"reports"`, 1))
			return ""
		}, state{core.CoveragePassing, true}, state{core.CoveragePassing, true}},
		{"a stamp at head on the current text, no local results", func(t *testing.T, f *fixture) string {
			f.ingestAt(f.commit("change NextID"), both)
			stamp(t, f, "ACME-SP-0001.R1", "ACME-SP-0001.R2")
			if err := os.Remove(f.store.Path()); err != nil {
				t.Fatal(err)
			}
			return ""
		}, state{core.CoveragePassing, false}, state{core.CoveragePassing, false}},
		{"a stamp at the base, no local results", func(t *testing.T, f *fixture) string {
			stamp(t, f, "ACME-SP-0001.R2")
			if err := os.Remove(f.store.Path()); err != nil {
				t.Fatal(err)
			}
			f.commit("change NextID and stamp R2")
			return ""
		}, state{core.CoverageUntested, false}, state{core.CoveragePassing, true}},
		{"a pending MODIFIED delta, tests not re-run", func(t *testing.T, f *fixture) string {
			f.commit("change NextID")
			modifyR1(t, f)
			return ""
		}, state{core.CoverageSuspect, true}, state{core.CoveragePassing, true}},
		{"a pending MODIFIED delta, tests re-run at head", func(t *testing.T, f *fixture) string {
			f.ingestAt(f.commit("change NextID"), both)
			modifyR1(t, f)
			return ""
		}, state{core.CoveragePassing, false}, state{core.CoveragePassing, false}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			head := tc.setup(t, f)
			if _, err := f.vlt.Reload(context.Background()); err != nil {
				t.Fatal(err)
			}
			q := core.ImpactQuery{Base: f.base, Head: head, Tiers: []int{1, 2}}
			first := f.impact(f.resolver(fixtureGraph(), nil), q)
			got := map[string]state{}
			for _, h := range first.Hits {
				got[h.Ref.String()] = state{h.Status, h.Suspect}
			}
			if got["ACME-SP-0001.R1"] != tc.r1 || got["ACME-SP-0001.R2"] != tc.r2 {
				t.Errorf("R1, R2 = %+v, %+v, want %+v, %+v (hits %+v)",
					got["ACME-SP-0001.R1"], got["ACME-SP-0001.R2"], tc.r1, tc.r2, first.Hits)
			}
			// Deterministic: the same diff and the same results, the same answer.
			again := f.impact(f.resolver(fixtureGraph(), nil), q)
			a, _ := json.Marshal(first)
			b, _ := json.Marshal(again)
			if !bytes.Equal(a, b) {
				t.Errorf("two runs differ:\n%s\n%s", a, b)
			}
		})
	}
}

// stamp writes the verified: stamp of refs from the ingested results, as
// `gintrack spec verify --commit` does.
func stamp(t *testing.T, f *fixture, refs ...string) {
	t.Helper()
	params, err := json.Marshal(map[string]any{"refs": refs, "by": "ci"})
	if err != nil {
		t.Fatal(err)
	}
	var env struct {
		OK     bool            `json:"ok"`
		Result json.RawMessage `json:"result"`
	}
	out := f.vlt.Call("requirement.stamp", string(params))
	if err := json.Unmarshal([]byte(out), &env); err != nil || !env.OK {
		t.Fatalf("requirement.stamp: %v %s", err, out)
	}
	if !strings.Contains(string(env.Result), `"verified"`) {
		t.Fatalf("requirement.stamp wrote nothing: %s", env.Result)
	}
}

// modifyR1 adds an open story whose unapplied Spec Delta modifies R1.
func modifyR1(t *testing.T, f *fixture) {
	t.Helper()
	f.write("docs/.pmngr/stories/ACME-US-0002-allocate-by-scan-again.md", `---
id: ACME-US-0002
type: story
title: Allocate by scan again
status: in_progress
created: 2026-01-01T00:00:00Z
updated: 2026-01-01T00:00:00Z
---

## Spec Delta

### MODIFIED ACME-SP-0001.R1 — Allocate by scan

The allocator SHALL allocate max + 2.
`)
	if _, err := f.vlt.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}
}
