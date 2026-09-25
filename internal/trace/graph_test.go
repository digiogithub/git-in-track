package trace

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/digiogithub/git-in-track/internal/core"
	"github.com/digiogithub/git-in-track/internal/core/osfs"
)

// graphFixture copies testdata/graph into a temporary repository, dropping
// the ".txt" suffix every fixture file carries so that a real scan of this
// repository never reads them as code, specs or a project.
func graphFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	src := filepath.Join("testdata", "graph")
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		dst := filepath.Join(root, strings.TrimSuffix(rel, ".txt"))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		return os.WriteFile(dst, data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// fixtureIndex builds the backlog index of a fixture repository.
func fixtureIndex(t *testing.T, root string) *core.Index {
	t.Helper()
	fsys, err := osfs.New(root)
	if err != nil {
		t.Fatal(err)
	}
	projects, err := core.DiscoverProjects(fsys, ".")
	if err != nil {
		t.Fatal(err)
	}
	ix := core.NewIndex(fsys, projects)
	if _, err := ix.Build(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	return ix
}

// fixtureGraph scans a fixture repository and builds its graph.
func fixtureGraph(t *testing.T) (*Graph, string, *core.Index) {
	t.Helper()
	root := graphFixture(t)
	ix := fixtureIndex(t, root)
	c, err := Scan(context.Background(), root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	g, err := BuildGraph(ix, c.Markers(), os.DirFS(root))
	if err != nil {
		t.Fatal(err)
	}
	return g, root, ix
}

func mustRef(t *testing.T, s string) core.RequirementRef {
	t.Helper()
	ref, err := core.ParseRequirementRef(s)
	if err != nil {
		t.Fatal(err)
	}
	return ref
}

// edgeStrings renders edges as "<trace-ref> <sources> <lines>".
func edgeStrings(es []core.TraceEdge) []string {
	out := []string{}
	for _, e := range es {
		s := e.Ref.String() + " " + string(e.Role) + " " + e.TraceRef()
		for _, src := range e.Sources {
			s += " " + string(src)
		}
		for _, ln := range e.Lines {
			s += " L" + itoa(ln)
		}
		out = append(out, s)
	}
	return out
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func workStrings(ws []core.TraceWork) []string {
	out := []string{}
	for _, w := range ws {
		s := string(w.ID) + " " + string(w.Kind)
		if w.WholeSpec {
			s += " spec"
		}
		out = append(out, s)
	}
	return out
}

func TestGraphRequirement(t *testing.T) {
	t.Parallel()
	g, _, _ := fixtureGraph(t)
	tests := []struct {
		ref    string
		code   []string
		tests  []string
		work   []string
		broken []string
	}{
		{
			ref: "ACME-SP-0001.R1",
			code: []string{
				"ACME-SP-0001.R1 code config/alloc.yaml marker trace L1",
				"ACME-SP-0001.R1 code src/alloc.go#NextID marker trace L3",
			},
			tests: []string{"ACME-SP-0001.R1 tests src/alloc_test.go#TestNextID/stale_counter marker trace L7"},
			// ACME-US-0003 links both the requirement and the spec: the
			// direct link wins. ACME-US-0002 modifies the whole spec.
			work: []string{"ACME-US-0001 implements", "ACME-US-0002 modifies spec", "ACME-US-0003 implements"},
		},
		{
			ref: "ACME-SP-0001.R2",
			code: []string{
				"ACME-SP-0001.R2 code config/alloc.yaml#anything trace",
				"ACME-SP-0001.R2 code src/alloc.go#Missing trace",
				"ACME-SP-0001.R2 code src/alloc.go#helper marker L9",
				"ACME-SP-0001.R2 code src/gone.go trace",
			},
			tests: []string{},
			// ACME-US-0004 is deleted.
			work: []string{"ACME-US-0002 modifies spec", "ACME-US-0003 implements spec"},
			// A YAML file declares no symbols, so its #anything is never broken.
			broken: []string{
				"trace.code src/alloc.go#Missing: trace.code of ACME-SP-0001.R2: src/alloc.go declares no symbol Missing",
				"trace.code src/gone.go: trace.code of ACME-SP-0001.R2: src/gone.go does not exist",
			},
		},
		{
			ref:   "ACME-SP-0001.R3",
			code:  []string{},
			tests: []string{"ACME-SP-0001.R3 tests web/alloc.test.ts#alloc > allocates marker L4"},
			work:  []string{"ACME-US-0002 modifies spec", "ACME-US-0003 implements spec"},
		},
		{
			ref:   "ACME-SP-0002.R1",
			code:  []string{"ACME-SP-0002.R1 code src/alloc.go#Counter.Bump marker L15"},
			tests: []string{},
			work:  []string{},
		},
	}
	for _, tc := range tests {
		t.Run(tc.ref, func(t *testing.T) {
			t.Parallel()
			tr, ok := g.Requirement(mustRef(t, tc.ref))
			if !ok {
				t.Fatalf("Requirement(%s) not found", tc.ref)
			}
			if tr.Project != "ACME" {
				t.Errorf("Project = %q", tr.Project)
			}
			if got := edgeStrings(tr.Code); !reflect.DeepEqual(got, tc.code) {
				t.Errorf("Code = %q\nwant %q", got, tc.code)
			}
			if got := edgeStrings(tr.Tests); !reflect.DeepEqual(got, tc.tests) {
				t.Errorf("Tests = %q\nwant %q", got, tc.tests)
			}
			if got := workStrings(tr.Work); !reflect.DeepEqual(got, tc.work) {
				t.Errorf("Work = %q\nwant %q", got, tc.work)
			}
			var broken []string
			for _, b := range tr.Broken {
				if b.Code != core.CodeWarnTraceBroken || b.Severity != core.SeverityWarning {
					t.Errorf("broken finding %+v is not a W-TRACE-BROKEN warning", b)
				}
				broken = append(broken, b.Field+" "+b.Entry+": "+b.Message)
			}
			if !reflect.DeepEqual(broken, tc.broken) {
				t.Errorf("Broken = %q\nwant %q", broken, tc.broken)
			}
		})
	}
	if _, ok := g.Requirement(mustRef(t, "ACME-SP-0009.R1")); ok {
		t.Error("a dangling marker's requirement is in the graph")
	}
	if n := len(g.Broken()); n != 2 {
		t.Errorf("Broken() has %d findings, want 2", n)
	}
	var refs []string
	for _, tr := range g.Requirements() {
		refs = append(refs, tr.Ref.String())
	}
	if want := []string{"ACME-SP-0001.R1", "ACME-SP-0001.R2", "ACME-SP-0001.R3", "ACME-SP-0002.R1"}; !reflect.DeepEqual(refs, want) {
		t.Errorf("Requirements() = %q, want %q", refs, want)
	}
}

func TestGraphReverse(t *testing.T) {
	t.Parallel()
	g, _, _ := fixtureGraph(t)
	if want := []string{"config/alloc.yaml", "src/alloc.go", "src/alloc_test.go", "src/gone.go", "web/alloc.test.ts"}; !reflect.DeepEqual(g.Paths(), want) {
		t.Errorf("Paths() = %q, want %q", g.Paths(), want)
	}
	tests := []struct {
		name, path, symbol string
		all                bool // ForPath instead of ForSymbol
		want               []string
	}{
		{"whole file", "src/alloc.go", "", true, []string{
			"ACME-SP-0002.R1 code src/alloc.go#Counter.Bump marker L15",
			"ACME-SP-0001.R2 code src/alloc.go#Missing trace",
			"ACME-SP-0001.R1 code src/alloc.go#NextID marker trace L3",
			"ACME-SP-0001.R2 code src/alloc.go#helper marker L9",
		}},
		{"symbol", "src/alloc.go", "NextID", false, []string{"ACME-SP-0001.R1 code src/alloc.go#NextID marker trace L3"}},
		{"method under its type", "src/alloc.go", "Counter", false, []string{"ACME-SP-0002.R1 code src/alloc.go#Counter.Bump marker L15"}},
		{"parent test reaches its sub-test", "src/alloc_test.go", "TestNextID", false, []string{
			"ACME-SP-0001.R1 tests src/alloc_test.go#TestNextID/stale_counter marker trace L7",
		}},
		{"sibling sub-test does not", "src/alloc_test.go", "TestNextID/fresh", false, []string{}},
		{"whole-file edge answers any symbol", "config/alloc.yaml", "x", false, []string{"ACME-SP-0001.R1 code config/alloc.yaml marker trace L1"}},
		{"untraced path", "README.md", "", true, []string{}},
		{"unclean path", "./src//alloc.go", "helper", false, []string{"ACME-SP-0001.R2 code src/alloc.go#helper marker L9"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var got []core.TraceEdge
			if tc.all {
				got = g.ForPath(tc.path)
			} else {
				got = g.ForSymbol(tc.path, tc.symbol)
			}
			if s := edgeStrings(got); !reflect.DeepEqual(s, tc.want) {
				t.Errorf("got %q\nwant %q", s, tc.want)
			}
		})
	}
}

func hitStrings(hs []core.TraceHit) []string {
	out := []string{}
	for _, h := range hs {
		s := h.Ref.String() + " " + h.TraceRef() + " " + h.Reason
		if h.Changed != "" {
			s += " (" + h.Changed + ")"
		}
		out = append(out, s)
	}
	return out
}

func TestGraphTouching(t *testing.T) {
	t.Parallel()
	g, root, _ := fixtureGraph(t)
	tree := os.DirFS(root)
	tests := []struct {
		name    string
		changes []core.TraceChange
		want    []string
	}{
		{"body of a traced function", []core.TraceChange{{Path: "src/alloc.go", Lines: []core.LineSpan{{Start: 5, Count: 1}}}},
			[]string{"ACME-SP-0001.R1 src/alloc.go#NextID symbol (NextID)"}},
		{"marker line itself", []core.TraceChange{{Path: "src/alloc.go", Lines: []core.LineSpan{{Start: 3, Count: 1}}}},
			[]string{"ACME-SP-0001.R1 src/alloc.go#NextID marker"}},
		{"outside every declaration", []core.TraceChange{{Path: "src/alloc.go", Lines: []core.LineSpan{{Start: 1, Count: 1}}}},
			[]string{}},
		{"two hunks, pure deletion", []core.TraceChange{{Path: "src/alloc.go", Lines: []core.LineSpan{{Start: 10, Count: 0}, {Start: 16, Count: 1}}}},
			[]string{
				"ACME-SP-0002.R1 src/alloc.go#Counter.Bump symbol (Counter.Bump)",
				"ACME-SP-0001.R2 src/alloc.go#helper symbol (helper)",
			}},
		{"a removed line inside a traced function", []core.TraceChange{{Path: "src/alloc.go", Lines: []core.LineSpan{{Start: 4, Count: 0}}}},
			[]string{"ACME-SP-0001.R1 src/alloc.go#NextID symbol (NextID)"}},
		{"lines removed after a traced function", []core.TraceChange{{Path: "src/alloc.go", Lines: []core.LineSpan{{Start: 6, Count: 0}}}},
			[]string{}},
		{"lines removed from the top of the file", []core.TraceChange{{Path: "src/alloc.go", Lines: []core.LineSpan{{Start: 0, Count: 0}}}},
			[]string{}},
		{"a sub-test removed between two sub-tests", []core.TraceChange{{Path: "src/alloc_test.go", Lines: []core.LineSpan{{Start: 11, Count: 0}}}},
			[]string{"ACME-SP-0001.R1 src/alloc_test.go#TestNextID/stale_counter symbol (TestNextID)"}},
		{"sub-test body", []core.TraceChange{{Path: "src/alloc_test.go", Lines: []core.LineSpan{{Start: 8, Count: 3}}}},
			[]string{"ACME-SP-0001.R1 src/alloc_test.go#TestNextID/stale_counter symbol (TestNextID/stale_counter)"}},
		{"sibling sub-test", []core.TraceChange{{Path: "src/alloc_test.go", Lines: []core.LineSpan{{Start: 12, Count: 1}}}},
			[]string{}},
		{"ts test title path", []core.TraceChange{{Path: "web/alloc.test.ts", Lines: []core.LineSpan{{Start: 6, Count: 1}}}},
			[]string{"ACME-SP-0001.R3 web/alloc.test.ts#alloc > allocates symbol (alloc > allocates)"}},
		{"whole-file edge", []core.TraceChange{{Path: "config/alloc.yaml", Lines: []core.LineSpan{{Start: 2, Count: 1}}}},
			[]string{"ACME-SP-0001.R1 config/alloc.yaml file"}},
		{"no lines is the whole file", []core.TraceChange{{Path: "src/alloc_test.go"}},
			[]string{"ACME-SP-0001.R1 src/alloc_test.go#TestNextID/stale_counter file"}},
		{"deleted file", []core.TraceChange{{Path: "src/gone.go", Lines: []core.LineSpan{{Start: 1, Count: 1}}}},
			[]string{"ACME-SP-0001.R2 src/gone.go file"}},
		{"explicit symbols", []core.TraceChange{{Path: "src/alloc.go", Symbols: []string{"Counter"}}},
			[]string{"ACME-SP-0002.R1 src/alloc.go#Counter.Bump symbol (Counter)"}},
		{"rename", []core.TraceChange{{Path: "src/moved.go", OldPath: "src/gone.go"}},
			[]string{"ACME-SP-0001.R2 src/gone.go renamed"}},
		{"untraced file", []core.TraceChange{{Path: "README.md"}}, []string{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := hitStrings(g.Touching(tc.changes, tree)); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Touching() = %q\nwant %q", got, tc.want)
			}
		})
	}
}

// TestGraphStable builds the graph twice from the same state and requires
// byte-identical answers.
func TestGraphStable(t *testing.T) {
	t.Parallel()
	root := graphFixture(t)
	ix := fixtureIndex(t, root)
	var prev []byte
	for i := 0; i < 3; i++ {
		c, err := Scan(context.Background(), root, Options{})
		if err != nil {
			t.Fatal(err)
		}
		g, err := BuildGraph(ix, c.Markers(), os.DirFS(root))
		if err != nil {
			t.Fatal(err)
		}
		changes := []core.TraceChange{{Path: "src/alloc.go"}, {Path: "web/alloc.test.ts"}}
		data, err := json.Marshal(map[string]any{
			"reqs": g.Requirements(), "broken": g.Broken(), "paths": g.Paths(),
			"touching": g.Touching(changes, os.DirFS(root)),
		})
		if err != nil {
			t.Fatal(err)
		}
		if prev != nil && string(prev) != string(data) {
			t.Fatalf("build %d differs:\n%s\nvs\n%s", i, prev, data)
		}
		prev = data
	}
}

func TestEngine(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	root := graphFixture(t)
	ix := fixtureIndex(t, root)
	e := NewEngine(root, EngineOptions{})

	tr, err := e.TraceRequirement(ctx, ix, mustRef(t, "ACME-SP-0002.R1"))
	if err != nil || len(tr.Code) != 1 {
		t.Fatalf("TraceRequirement() = %+v, %v", tr, err)
	}
	if _, err := e.TraceRequirement(ctx, ix, mustRef(t, "ACME-SP-0002.R7")); !errors.Is(err, core.ErrItemNotFound) {
		t.Errorf("unknown requirement: err = %v, want ErrItemNotFound", err)
	}
	g1, _ := e.Graph(ctx, ix)
	if g2, _ := e.Graph(ctx, ix); g1 != g2 {
		t.Error("an unchanged repository rebuilt the graph")
	}

	// Move the markers: the diff-driven query rescans the path, reports the
	// edges that went away from the graph as it stood, and the new one.
	// NextID keeps its trace: entry, so its edge survives and is hit by symbol.
	src := "package alloc\n\nfunc NextID() int {\n\treturn 1\n}\n\n" +
		"// Implements: ACME-SP-0002.R1\nfunc helper() int {\n\treturn 2\n}\n\n" +
		"type Counter struct{}\n\nfunc (c *Counter) Bump() {}\n"
	if err := os.WriteFile(filepath.Join(root, "src", "alloc.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	hits, err := e.TraceTouching(ctx, ix, []core.TraceChange{{Path: "src/alloc.go", Lines: []core.LineSpan{{Start: 3, Count: 12}}}})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"ACME-SP-0002.R1 src/alloc.go#Counter.Bump removed",
		"ACME-SP-0001.R1 src/alloc.go#NextID symbol (NextID)",
		"ACME-SP-0001.R2 src/alloc.go#helper removed",
		"ACME-SP-0002.R1 src/alloc.go#helper marker",
	}
	if got := hitStrings(hits); !reflect.DeepEqual(got, want) {
		t.Errorf("TraceTouching() = %q\nwant %q", got, want)
	}
	tr, _ = e.TraceRequirement(ctx, ix, mustRef(t, "ACME-SP-0002.R1"))
	if got := edgeStrings(tr.Code); !reflect.DeepEqual(got, []string{"ACME-SP-0002.R1 code src/alloc.go#helper marker L7"}) {
		t.Errorf("after the update Code = %q", got)
	}
	if hits, err := e.TraceTouching(ctx, ix, nil); err != nil || hits == nil || len(hits) != 0 {
		t.Errorf("TraceTouching(nil) = %v, %v; want an empty, non-nil list", hits, err)
	}
}
