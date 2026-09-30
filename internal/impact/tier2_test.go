package impact

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/digiogithub/git-in-track/internal/core"
	"github.com/digiogithub/git-in-track/internal/pando"
)

// The tier-2 precision cases of the spec impact benchmark re-run with Pando
// (GIT-US-0161, docs/research/2026-09-25-spec-impact-benchmark.md §9.3),
// replayed in a regression fixture (GIT-US-0166):
//
//   - P2: Pando resolves a callee by name, so AddComment, which calls the
//     unchanged CommentKind.Valid, came back as a caller of the changed
//     LinkKind.Valid. P4 did the same with two functions named add.
//   - P1: the production caller UpdateRequirement carries the markers of R3
//     and R4, whose only tier-1 reason is their changed verifying test; its
//     call: reason flipped both from test-only to behaviour.
//   - P4: a caller in a test file reached a requirement as behaviour.
const (
	fxLinksSpecPath = "docs/.pmngr/specs/ACME-SP-0003-links-and-revs.md"
	fxLinksSpec     = `---
id: ACME-SP-0003
type: spec
title: Links and revisions
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
  R5:
    status: todo
  R6:
    status: todo
  R7:
    status: todo
  R8:
    status: todo
---

## Requirements

### ACME-SP-0003.R1 — Known link kinds

A link kind SHALL be one of the known kinds.

### ACME-SP-0003.R2 — Comment kinds

A comment SHALL carry a known comment kind.

### ACME-SP-0003.R3 — Quote the rev

A requirement write SHALL quote the requirement rev.

### ACME-SP-0003.R4 — Refuse a stale rev

A stale requirement write SHALL be refused.

### ACME-SP-0003.R5 — Name the conflicts

A refused requirement write SHALL name the conflicting fields.

### ACME-SP-0003.R6 — Fake writes

The fake store SHALL write like the real one.

### ACME-SP-0003.R7 — Inbound refs

The index SHALL list the inbound refs of a requirement.

### ACME-SP-0003.R8 — Apply a patch

A patch SHALL apply its fields.
`
	fxKinds = `package alloc

// LinkKind is the kind of a link.
type LinkKind string

// Implements: ACME-SP-0003.R1
func (k LinkKind) Valid() bool {
	return k == "blocks"
}

// CommentKind is the kind of a comment.
type CommentKind string

// Valid reports whether the comment kind is known.
func (k CommentKind) Valid() bool {
	return k == "note"
}

// Implements: ACME-SP-0003.R2
func AddComment(k CommentKind) bool {
	return k.Valid()
}

func add(ids []int, id int) []int {
	return append(ids, id)
}

type refSet map[string]bool

func (s refSet) add(ref string) {
	s[ref] = true
}

// Implements: ACME-SP-0003.R7
func requirementRefsTo(refs []string) refSet {
	s := refSet{}
	for _, r := range refs {
		s.add(r)
	}
	return s
}
`
	fxStore = `package alloc

// Implements: ACME-SP-0003.R3, ACME-SP-0003.R4
func UpdateRequirement(ids []int) int {
	return conflicts(ids)
}

// Implements: ACME-SP-0003.R8
func ApplyPatch(ids []int) int {
	return conflicts(ids)
}

// Implements: ACME-SP-0003.R5
func conflicts(ids []int) int {
	return len(ids)
}
`
	fxStoreTests = `package alloc

import "testing"

// Verifies: ACME-SP-0003.R3, ACME-SP-0003.R4, ACME-SP-0003.R5
func TestRevProtocol(t *testing.T) {
	t.Log("conflicts")
}

// Verifies: ACME-SP-0003.R8
func TestApplyPatch(t *testing.T) {
	t.Log("apply")
}

// Implements: ACME-SP-0003.R6
func fakeWrite(ids []int) int {
	return conflicts(ids)
}
`
)

// finderGraph is a fakeGraph that also pins a name to its definitions, as
// code_find_symbol does.
type finderGraph struct {
	*fakeGraph
	defs  map[string][]pando.Symbol
	err   error    // the answer of every FindSymbol, when set
	tried []string // every name FindSymbol was called with
	found []string // the names it answered
}

func (g *finderGraph) FindSymbol(_ context.Context, projectID, name string, _ pando.FindSymbolOptions) ([]pando.Symbol, error) {
	g.tried = append(g.tried, name)
	if g.err != nil {
		return nil, g.err
	}
	if projectID != "acme" {
		return nil, pando.ErrInvalidOptions
	}
	g.found = append(g.found, name)
	return g.defs[name], nil
}

// precisionFixture commits the benchmark shapes on top of the base fixture
// and applies the diff: LinkKind.Valid, conflicts and the free add change,
// and so does TestRevProtocol, which verifies R3, R4 and R5.
func precisionFixture(t *testing.T) (*fixture, string) {
	t.Helper()
	f := newFixture(t)
	f.write("src/alloc.go", fxAlloc)
	f.write(fxLinksSpecPath, fxLinksSpec)
	f.write("src/links/kinds.go", fxKinds)
	f.write("src/links/store.go", fxStore)
	f.write("src/links/store_test.go", fxStoreTests)
	base := f.commit("links and revisions")
	if _, err := f.vlt.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.write("src/links/kinds.go", changedKinds())
	f.write("src/links/store.go", strings.Replace(fxStore, "return len(ids)", "return len(ids) + 0", 1))
	f.write("src/links/store_test.go", strings.Replace(fxStoreTests, `t.Log("conflicts")`, `t.Log("named conflicts")`, 1))
	return f, base
}

// changedKinds is kinds.go with LinkKind.Valid and the free add changed.
func changedKinds() string {
	return strings.NewReplacer(
		`return k == "blocks"`, `return k == "blocks" || k == "relates_to"`,
		"return append(ids, id)", "return append(append([]int(nil), ids...), id)",
	).Replace(fxKinds)
}

// precisionGraph answers like Pando did on the benchmark: by name.
func precisionGraph() *fakeGraph {
	return &fakeGraph{callers: map[string][]pando.ImpactCaller{
		"Valid": {
			{Name: "AddComment", FilePath: "src/links/kinds.go", StartLine: lineOf(fxKinds, "func AddComment"), Depth: 1},
		},
		"add": {
			{Name: "requirementRefsTo", FilePath: "src/links/kinds.go", StartLine: lineOf(fxKinds, "func requirementRefsTo"), Depth: 1},
		},
		"conflicts": {
			{Name: "UpdateRequirement", FilePath: "src/links/store.go", StartLine: lineOf(fxStore, "func UpdateRequirement"), Depth: 1},
			{Name: "ApplyPatch", FilePath: "src/links/store.go", StartLine: lineOf(fxStore, "func ApplyPatch"), Depth: 1},
			{Name: "fakeWrite", FilePath: "src/links/store_test.go", StartLine: lineOf(fxStoreTests, "func fakeWrite"), Depth: 1},
		},
	}}
}

// precisionDefs are the definitions code_find_symbol pins each name to.
func precisionDefs() map[string][]pando.Symbol {
	def := func(name, namePath, file, text, line string) pando.Symbol {
		return pando.Symbol{Name: name, NamePath: namePath, SymbolType: "method", FilePath: file, StartLine: lineOf(text, line)}
	}
	fn := func(s pando.Symbol) pando.Symbol { s.SymbolType = "function"; return s }
	return map[string][]pando.Symbol{
		"Valid": {
			def("Valid", "LinkKind/Valid", "src/links/kinds.go", fxKinds, "func (k LinkKind) Valid"),
			def("Valid", "CommentKind/Valid", "src/links/kinds.go", fxKinds, "func (k CommentKind) Valid"),
		},
		"add": {
			fn(def("add", "add", "src/links/kinds.go", fxKinds, "func add(")),
			def("add", "refSet/add", "src/links/kinds.go", fxKinds, "func (s refSet) add"),
			// A field of the same name is not a callee.
			{Name: "add", NamePath: "ops/add", SymbolType: "field", FilePath: "src/links/kinds.go", StartLine: 1},
		},
		"conflicts": {
			fn(def("conflicts", "conflicts", "src/links/store.go", fxStore, "func conflicts")),
		},
	}
}

// TestImpactTier2Precision pins the three tier-2 rules of GIT-US-0166.
func TestImpactTier2Precision(t *testing.T) {
	f, base := precisionFixture(t)
	q := core.ImpactQuery{Base: base, Tiers: []int{1, 2}}
	kinds := func(res core.ImpactResult) map[string]core.ImpactKind {
		out := map[string]core.ImpactKind{}
		for _, h := range res.Hits {
			out[h.Ref.String()] = h.Kind
		}
		return out
	}
	check := func(t *testing.T, got, want map[string]core.ImpactKind) {
		t.Helper()
		if len(got) != len(want) {
			t.Errorf("hits = %v, want %v", got, want)
		}
		for ref, kind := range want {
			if k, ok := got[ref]; !ok || k != kind {
				t.Errorf("%s kind = %q (present %v), want %q", ref, k, ok, kind)
			}
		}
	}

	t.Run("the benchmark shapes with the callee pinned", func(t *testing.T) {
		graph := &finderGraph{fakeGraph: precisionGraph(), defs: precisionDefs()}
		res := f.impact(f.resolver(graph, nil), q)
		check(t, kinds(res), map[string]core.ImpactKind{
			// Tier 1: the changed code.
			"ACME-SP-0003.R1": core.ImpactKindBehaviour, // LinkKind.Valid
			"ACME-SP-0003.R5": core.ImpactKindBehaviour, // conflicts, and its changed test
			// P1: a caller carrying two markers does not flip the test-only
			// verdict of the changed TestRevProtocol.
			"ACME-SP-0003.R3": core.ImpactKindTestOnly,
			"ACME-SP-0003.R4": core.ImpactKindTestOnly,
			// A caller carrying one marker is its requirement's own code.
			"ACME-SP-0003.R8": core.ImpactKindBehaviour,
			// P4: a caller in a test file is test evidence.
			"ACME-SP-0003.R6": core.ImpactKindTestOnly,
			// P2 and P4: R2 (AddComment calls CommentKind.Valid) and R7
			// (requirementRefsTo calls refSet.add) are dropped.
		})
		if got := strings.Join(graph.found, ","); got != "Valid,add,conflicts" {
			t.Errorf("pinned %s, want Valid,add,conflicts", got)
		}
		if got := strings.Join(graph.asked, ","); got != "conflicts" {
			t.Errorf("asked for the callers of %s, want conflicts only: a shared name is not asked", got)
		}
		second := f.impact(f.resolver(&finderGraph{fakeGraph: precisionGraph(), defs: precisionDefs()}, nil), q)
		a, err := json.MarshalIndent(res, "", "  ")
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
		checkGolden(t, "impact_tier2_precision.golden.json", bytes.ReplaceAll(append(a, '\n'), []byte(base), []byte("<base>")))
	})

	t.Run("a name every changed definition shares is kept", func(t *testing.T) {
		defs := precisionDefs()
		// Both Valid methods changed: every caller of the name is a caller
		// of changed code.
		f.write("src/links/kinds.go", strings.Replace(changedKinds(), `return k == "note"`, `return k == "note" || k == "question"`, 1))
		defer f.write("src/links/kinds.go", changedKinds())
		graph := &finderGraph{fakeGraph: precisionGraph(), defs: defs}
		got := kinds(f.impact(f.resolver(graph, nil), q))
		if got["ACME-SP-0003.R2"] != core.ImpactKindBehaviour {
			t.Errorf("R2 kind = %q, want behaviour: AddComment calls a changed Valid", got["ACME-SP-0003.R2"])
		}
	})

	t.Run("a pando without code_find_symbol keeps every caller", func(t *testing.T) {
		got := kinds(f.impact(f.resolver(precisionGraph(), nil), q))
		for _, ref := range []string{"ACME-SP-0003.R2", "ACME-SP-0003.R7"} {
			if got[ref] != core.ImpactKindBehaviour {
				t.Errorf("%s kind = %q, want behaviour: nothing pins the name", ref, got[ref])
			}
		}
	})

	t.Run("a failing pin keeps the callers", func(t *testing.T) {
		graph := &finderGraph{fakeGraph: precisionGraph(), defs: precisionDefs(), err: pando.ErrToolFailed}
		res := f.impact(f.resolver(graph, nil), q)
		if res.Tiers[1].Status != core.ImpactTierOK {
			t.Errorf("tier 2 = %+v, want ok", res.Tiers[1])
		}
		if got := kinds(res); got["ACME-SP-0003.R2"] != core.ImpactKindBehaviour {
			t.Errorf("R2 kind = %q, want behaviour: nothing pinned Valid", got["ACME-SP-0003.R2"])
		}
		if len(graph.tried) != 1 {
			t.Errorf("code_find_symbol called %d times, want once: a failure turns pinning off", len(graph.tried))
		}
	})

	t.Run("an unreachable pando fails the tier", func(t *testing.T) {
		graph := &finderGraph{fakeGraph: &fakeGraph{err: pando.ErrUnreachable}, err: pando.ErrUnreachable}
		res := f.impact(f.resolver(graph, nil), q)
		if res.Tiers[1].Status != core.ImpactTierUnavailable {
			t.Errorf("tier 2 = %+v, want unavailable", res.Tiers[1])
		}
	})
}

// relatorGraph is a finderGraph that also answers code_related_files, the
// probe tier 2 runs when no changed name has a caller (GIT-US-0167).
type relatorGraph struct {
	*finderGraph
	related map[string][]pando.RelatedFile
	err     error    // the answer of every RelatedFiles, when set
	probed  []string // every file RelatedFiles was called with
}

func (g *relatorGraph) RelatedFiles(_ context.Context, projectID, path string, _ pando.RelatedFilesOptions) (pando.RelatedFilesResult, error) {
	g.probed = append(g.probed, path)
	if g.err != nil {
		return pando.RelatedFilesResult{}, g.err
	}
	if projectID != "acme" {
		return pando.RelatedFilesResult{}, pando.ErrInvalidOptions
	}
	return pando.RelatedFilesResult{Files: g.related[path]}, nil
}

// TestImpactTier2CodeGraph pins GIT-US-0167: a project indexed without call
// edges ([TokenOptimization] BuildCodeGraph = false) answers "No callers
// found" for every name, which is not the same answer as a real "no callers".
// Tier 2 tells them apart with code_related_files, and reports the first as
// unavailable, never as ok with no hits.
func TestImpactTier2CodeGraph(t *testing.T) {
	f, base := precisionFixture(t)
	q := core.ImpactQuery{Base: base, Tiers: []int{1, 2}}
	noCallers := func() *finderGraph {
		return &finderGraph{fakeGraph: &fakeGraph{}, defs: precisionDefs()}
	}
	tier2Hits := func(res core.ImpactResult) int {
		n := 0
		for _, h := range res.Hits {
			if h.Tier == core.ImpactTierTransitive {
				n++
			}
		}
		return n
	}

	t.Run("a project without call edges is unavailable", func(t *testing.T) {
		graph := &relatorGraph{finderGraph: noCallers()}
		res := f.impact(f.resolver(graph, nil), q)
		tier := res.Tiers[1]
		if tier.Status != core.ImpactTierUnavailable || tier.Message != NoCallEdges {
			t.Fatalf("tier 2 = %+v, want unavailable with %q", tier, NoCallEdges)
		}
		if !strings.Contains(tier.Message, "BuildCodeGraph") {
			t.Errorf("message %q does not name BuildCodeGraph", tier.Message)
		}
		if res.Tiers[0].Status != core.ImpactTierOK {
			t.Errorf("tier 1 = %+v, want ok", res.Tiers[0])
		}
		if got := strings.Join(graph.probed, ","); got != "src/links/kinds.go,src/links/store.go,src/links/store_test.go" {
			t.Errorf("probed %s, want every changed file in order", got)
		}
	})

	t.Run("a real no-callers answer is ok with no hits", func(t *testing.T) {
		graph := &relatorGraph{finderGraph: noCallers(), related: map[string][]pando.RelatedFile{
			"src/links/store.go": {{FilePath: "src/links/store_test.go", Score: 0.8, Reasons: []string{"calls"}}},
		}}
		res := f.impact(f.resolver(graph, nil), q)
		if res.Tiers[1].Status != core.ImpactTierOK || res.Tiers[1].Message != "" {
			t.Fatalf("tier 2 = %+v, want ok", res.Tiers[1])
		}
		if n := tier2Hits(res); n != 0 {
			t.Errorf("%d tier-2 hits, want none", n)
		}
		if got := strings.Join(graph.probed, ","); got != "src/links/kinds.go,src/links/store.go" {
			t.Errorf("probed %s, want the probe to stop at the first coupled file", got)
		}
	})

	t.Run("callers prove the graph without a probe", func(t *testing.T) {
		graph := &relatorGraph{finderGraph: &finderGraph{fakeGraph: precisionGraph(), defs: precisionDefs()}}
		res := f.impact(f.resolver(graph, nil), q)
		if res.Tiers[1].Status != core.ImpactTierOK {
			t.Fatalf("tier 2 = %+v, want ok", res.Tiers[1])
		}
		if len(graph.probed) != 0 {
			t.Errorf("probed %v, want no probe: a caller came back", graph.probed)
		}
	})

	t.Run("an unreachable probe is unavailable", func(t *testing.T) {
		graph := &relatorGraph{finderGraph: noCallers(), err: pando.ErrUnreachable}
		res := f.impact(f.resolver(graph, nil), q)
		if res.Tiers[1].Status != core.ImpactTierUnavailable || res.Tiers[1].Message != "Pando is unreachable" {
			t.Errorf("tier 2 = %+v, want unavailable (Pando is unreachable)", res.Tiers[1])
		}
	})

	t.Run("a failing probe is no evidence of a graph", func(t *testing.T) {
		graph := &relatorGraph{finderGraph: noCallers(), err: pando.ErrToolFailed}
		res := f.impact(f.resolver(graph, nil), q)
		if res.Tiers[1].Status != core.ImpactTierUnavailable || res.Tiers[1].Message != NoCallEdges {
			t.Errorf("tier 2 = %+v, want unavailable with %q", res.Tiers[1], NoCallEdges)
		}
	})

	t.Run("a pando that cannot probe keeps ok", func(t *testing.T) {
		res := f.impact(f.resolver(noCallers(), nil), q)
		if res.Tiers[1].Status != core.ImpactTierOK {
			t.Errorf("tier 2 = %+v, want ok: without code_related_files the graph cannot be checked", res.Tiers[1])
		}
	})
}

// The P3 replay of the benchmark (GIT-US-0181, §10.6): the diff changes
// LinkKind.Inverse, which carries no marker. Its production caller
// validateItemLinks carries the markers of R1 and R3, and an unchanged test
// that verifies both calls Inverse too. GIT-US-0166 ranked both hits
// test-only, although validateItemLinks runs the changed code.
const (
	fxInverse = `package links

// LinkKind is the kind of a link.
type LinkKind string

func (k LinkKind) Inverse() LinkKind {
	return k
}
`
	fxValidate = `package links

// Implements: ACME-SP-0003.R1, ACME-SP-0003.R3
func validateItemLinks(kinds []LinkKind) []LinkKind {
	var out []LinkKind
	for _, k := range kinds {
		out = append(out, k.Inverse())
	}
	return out
}
`
	fxValidateTests = `package links

import "testing"

// Verifies: ACME-SP-0003.R1, ACME-SP-0003.R3
func TestLinkKindValidAndInverse(t *testing.T) {
	if LinkKind("a").Inverse() != "a" {
		t.Fatal("inverse")
	}
}
`
)

func TestImpactTier2NarrowCallerOutranksTestCaller(t *testing.T) {
	// setup commits the P3 shapes with the given markers on validateItemLinks
	// and returns the fixture, the base commit and the callers Pando reports.
	setup := func(t *testing.T, markers string) (*fixture, string, []pando.ImpactCaller) {
		validate := strings.Replace(fxValidate, "ACME-SP-0003.R1, ACME-SP-0003.R3", markers, 1)
		f := newFixture(t)
		f.write(fxLinksSpecPath, fxLinksSpec)
		f.write("src/links/kind.go", fxInverse)
		f.write("src/links/validate.go", validate)
		f.write("src/links/validate_test.go", fxValidateTests)
		base := f.commit("link kinds")
		if _, err := f.vlt.Reload(context.Background()); err != nil {
			t.Fatal(err)
		}
		f.write("src/links/kind.go", strings.Replace(fxInverse, "return k", `return k + "_"`, 1))
		return f, base, []pando.ImpactCaller{
			{Name: "validateItemLinks", FilePath: "src/links/validate.go", StartLine: lineOf(validate, "func validateItemLinks"), Depth: 1},
			{Name: "TestLinkKindValidAndInverse", FilePath: "src/links/validate_test.go", StartLine: lineOf(fxValidateTests, "func TestLinkKindValidAndInverse"), Depth: 1},
		}
	}
	kinds := func(f *fixture, base string, callers []pando.ImpactCaller) map[string]core.ImpactKind {
		graph := &fakeGraph{callers: map[string][]pando.ImpactCaller{"Inverse": callers}}
		res := f.impact(f.resolver(graph, nil), core.ImpactQuery{Base: base, Tiers: []int{1, 2}})
		got := map[string]core.ImpactKind{}
		for _, h := range res.Hits {
			got[h.Ref.String()] = h.Kind
		}
		return got
	}

	t.Run("a caller of two requirements flips a test caller to behaviour", func(t *testing.T) {
		f, base, callers := setup(t, "ACME-SP-0003.R1, ACME-SP-0003.R3")
		got := kinds(f, base, callers)
		for _, ref := range []string{"ACME-SP-0003.R1", "ACME-SP-0003.R3"} {
			if got[ref] != core.ImpactKindBehaviour {
				t.Errorf("%s kind = %q, want behaviour: validateItemLinks calls the changed Inverse", ref, got[ref])
			}
		}
	})

	t.Run("a caller of three requirements does not", func(t *testing.T) {
		f, base, callers := setup(t, "ACME-SP-0003.R1, ACME-SP-0003.R3, ACME-SP-0003.R4")
		got := kinds(f, base, callers)
		for _, ref := range []string{"ACME-SP-0003.R1", "ACME-SP-0003.R3"} {
			if got[ref] != core.ImpactKindTestOnly {
				t.Errorf("%s kind = %q, want test-only: the caller carries three requirements", ref, got[ref])
			}
		}
	})

	t.Run("a test caller alone stays test-only", func(t *testing.T) {
		f, base, callers := setup(t, "ACME-SP-0003.R1, ACME-SP-0003.R3")
		got := kinds(f, base, callers[1:])
		if len(got) != 2 || got["ACME-SP-0003.R1"] != core.ImpactKindTestOnly || got["ACME-SP-0003.R3"] != core.ImpactKindTestOnly {
			t.Errorf("kinds = %v, want R1 and R3 test-only", got)
		}
	})
}
