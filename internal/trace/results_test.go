package trace

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/digiogithub/git-in-track/internal/core"
)

// resultTree is the working tree the report fixtures are resolved against.
func resultTree() fstest.MapFS {
	file := func(s string) *fstest.MapFile { return &fstest.MapFile{Data: []byte(s)} }
	return fstest.MapFS{
		"go.mod": file("// a module\nmodule example.com/acme // trailing\n\ngo 1.26\n"),
		"src/alloc_test.go": file(`package alloc

import "testing"

func TestNextID(t *testing.T) {
	t.Run("stale counter", func(t *testing.T) {})
	t.Run("fresh", func(t *testing.T) {})
}
`),
		"src/platform_linux_test.go": file("package alloc\n\nimport \"testing\"\n\nfunc TestPlatform(t *testing.T) {}\n"),
		"src/platform_other_test.go": file("package alloc\n\nimport \"testing\"\n\nfunc TestPlatform(t *testing.T) {}\n"),
		"cmd/tool/tool_test.go":      file("package main\n\nimport \"testing\"\n\nfunc TestTool(t *testing.T) {}\n"),
		"web/src/alloc.test.ts":      file("describe('alloc', () => {\n  it('allocates', () => {});\n});\n"),
		"tests/test_mod.py":          file("class TestCase:\n    def test_x(self):\n        pass\n\ndef test_free():\n    pass\n"),
	}
}

// resultRow renders a resolved result compactly for comparison.
func resultRow(r TestResult) string {
	s := string(r.Result) + " " + r.TraceRef()
	if r.Path == "" {
		s = string(r.Result) + " unmapped " + r.ID
	}
	if len(r.Ambiguous) > 0 {
		s += " ambiguous:" + strings.Join(r.Ambiguous, ",")
	}
	return s
}

func parseFixture(t *testing.T, name string, format ReportFormat) (ReportFormat, []RawResult) {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", "results", name))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	got, raws, err := ParseReport(f, format)
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	return got, raws
}

func TestParseAndResolveReports(t *testing.T) {
	tests := []struct {
		name    string
		fixture string
		format  ReportFormat // "" detects
		base    string
		want    ReportFormat
		rows    []string
		durs    map[string]time.Duration // by id, spot checks
	}{
		{
			name: "go test json with interleaved parallel sub-tests", fixture: "gotest.json", want: FormatGoTest,
			rows: []string{
				"fail src/alloc_test.go#TestNextID",
				"pass src/alloc_test.go#TestNextID/fresh",
				"fail src/alloc_test.go#TestNextID/stale_counter",
				"skip src/platform_linux_test.go#TestPlatform ambiguous:src/platform_other_test.go",
				"pass unmapped example.com/unknown#TestGhost",
				"pass cmd/tool/tool_test.go#TestTool",
			},
			durs: map[string]time.Duration{
				"example.com/acme/src#TestNextID/stale_counter": 20 * time.Millisecond,
				"other.org/tools/cmd/tool#TestTool":             500 * time.Millisecond,
			},
		},
		{
			name: "junit from go-junit-report", fixture: "junit-go.xml", want: FormatJUnit,
			rows: []string{
				"fail src/alloc_test.go#TestNextID",
				"pass src/alloc_test.go#TestNextID/fresh",
				"fail src/alloc_test.go#TestNextID/stale_counter",
				"skip src/platform_linux_test.go#TestPlatform ambiguous:src/platform_other_test.go",
			},
			durs: map[string]time.Duration{"example.com/acme/src#TestNextID": 30 * time.Millisecond},
		},
		{
			name: "junit from vitest with a base directory", fixture: "junit-vitest.xml", base: "web", want: FormatJUnit,
			rows: []string{
				"pass web/src/alloc.test.ts#alloc > allocates",
				"skip web/src/alloc.test.ts#alloc > later",
				"fail web/src/alloc.test.ts#alloc > reports",
			},
		},
		{
			name: "junit from vitest without the base stays unmapped", fixture: "junit-vitest.xml", want: FormatJUnit,
			rows: []string{
				"pass unmapped src/alloc.test.ts#alloc > allocates",
				"skip unmapped src/alloc.test.ts#alloc > later",
				"fail unmapped src/alloc.test.ts#alloc > reports",
			},
		},
		{
			name: "junit from pytest and an unmappable java case", fixture: "junit-pytest.xml", format: FormatJUnit, want: FormatJUnit,
			rows: []string{
				"pass unmapped com.acme.FooTest#testBar",
				"fail tests/test_mod.py#test_free",
				"pass tests/test_mod.py#TestCase.test_x",
			},
		},
		{
			name: "vitest json with absolute paths from a CI runner", fixture: "vitest.json", want: FormatVitest,
			rows: []string{
				"pass unmapped /elsewhere/gone.test.ts#top",
				"pass web/src/alloc.test.ts#alloc > allocates",
				"fail web/src/alloc.test.ts#alloc > edge > empty",
				"skip web/src/alloc.test.ts#alloc > later",
			},
			durs: map[string]time.Duration{
				"/home/runner/work/acme/acme/web/src/alloc.test.ts#alloc > allocates": 4500 * time.Microsecond,
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, raws := parseFixture(t, tc.fixture, tc.format)
			if got != tc.want {
				t.Fatalf("format = %q, want %q", got, tc.want)
			}
			r := NewTestResolver(resultTree(), "/repo", tc.base)
			var rows []string
			for _, raw := range raws {
				res := r.Resolve(raw)
				rows = append(rows, resultRow(res))
				if want, ok := tc.durs[res.ID]; ok && res.Duration != want {
					t.Errorf("duration of %s = %v, want %v", res.ID, res.Duration, want)
				}
			}
			if !reflect.DeepEqual(rows, tc.rows) {
				t.Errorf("rows:\n got %q\nwant %q", rows, tc.rows)
			}
		})
	}
}

func TestParseReportCollapsesRepeatedRuns(t *testing.T) {
	stream := strings.Join([]string{
		`{"Action":"pass","Package":"p","Test":"TestA","Elapsed":0.1}`,
		`{"Action":"fail","Package":"p","Test":"TestA","Elapsed":0.3}`,
		`{"Action":"pass","Package":"p","Test":"TestA","Elapsed":0.2}`,
		`{"Action":"skip","Package":"p","Test":"TestB"}`,
		`{"Action":"pass","Package":"p","Test":"TestB"}`,
	}, "\n")
	_, raws, err := ParseReport(strings.NewReader(stream), "")
	if err != nil {
		t.Fatal(err)
	}
	want := []RawResult{
		{Format: FormatGoTest, Scope: "p", Name: "TestA", Outcome: OutcomeFail, Duration: 300 * time.Millisecond},
		{Format: FormatGoTest, Scope: "p", Name: "TestB", Outcome: OutcomePass},
	}
	if !reflect.DeepEqual(raws, want) {
		t.Errorf("got %+v\nwant %+v", raws, want)
	}
}

func TestParseReportRejectsMalformedInput(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		format ReportFormat
		want   string
	}{
		{"empty input", "  \n", "", "empty"},
		{"plain text", "PASS\nok  example.com/x\n", "", "cannot tell the format"},
		{"unknown format", "{}", "tap", "unknown report format"},
		{"truncated go event", `{"Action":"pass","Package":"p","Test":"TestA"` + "\n", FormatGoTest, "line 1"},
		{"go event without an action", `{"Package":"p"}`, FormatGoTest, "without an Action"},
		{"go stream of plain text", "ok p 0.1s\n", FormatGoTest, "no go test -json event"},
		{"broken xml", `<testsuite><testcase name="a"></testsuite>`, "", "junit xml"},
		{"xml of another kind", `<html><body/></html>`, "", "root element is <html>"},
		{"json array for vitest", `[1,2]`, FormatVitest, "expected"},
		{"json object without testResults", `{"numTotalTests":1}`, "", "without testResults"},
		{"truncated vitest", `{"testResults":[{"name":"a","assertionResults":[`, "", "vitest json"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := ParseReport(strings.NewReader(tc.input), tc.format)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to mention %q", err, tc.want)
			}
		})
	}
	_, _, err := ParseReport(strings.NewReader(`<html/>`), "")
	if !errors.Is(err, ErrReportFormat) {
		t.Errorf("a non-report is not ErrReportFormat: %v", err)
	}
}

// TestParseGoTestLongOutputLine checks that a long output line — a large
// log dumped by a test — does not break the stream.
func TestParseGoTestLongOutputLine(t *testing.T) {
	long := strings.Repeat("x", 1<<20)
	stream := `{"Action":"output","Package":"p","Test":"TestA","Output":"` + long + `"}` + "\n" +
		`{"Action":"pass","Package":"p","Test":"TestA"}` + "\n"
	_, raws, err := ParseReport(strings.NewReader(stream), "")
	if err != nil || len(raws) != 1 || raws[0].Outcome != OutcomePass {
		t.Fatalf("got %+v, %v", raws, err)
	}
}

func TestResultSetMatch(t *testing.T) {
	results := []TestResult{
		{ID: "a", Path: "src/a_test.go", Symbol: "TestX", Result: OutcomePass},
		{ID: "b", Path: "src/a_test.go", Symbol: "TestX/one", Result: OutcomePass},
		{ID: "c", Path: "src/a_test.go", Symbol: "TestX/two", Result: OutcomeFail},
		{ID: "d", Path: "src/a_test.go", Symbol: "TestY", Result: OutcomePass},
		{ID: "e", Path: "web/a.test.ts", Symbol: "suite > case", Result: OutcomePass},
		{ID: "f", Symbol: "TestX", Result: OutcomeFail}, // unmapped: never matches
	}
	s := NewResultSet(results)
	tests := []struct {
		path, symbol string
		ids          []string
		match        string
	}{
		{"src/a_test.go", "TestX/one", []string{"b"}, MatchExact},
		{"src/a_test.go", "TestX", []string{"a", "b", "c"}, MatchExact},
		{"src/a_test.go", "TestX/three", []string{"a"}, MatchEnclosing},
		{"src/a_test.go", "TestY/sub/deeper", []string{"d"}, MatchEnclosing},
		{"src/a_test.go", "TestXY", nil, ""},
		{"src/a_test.go", "", []string{"a", "b", "c", "d"}, MatchFile},
		{"web/a.test.ts", "suite", []string{"e"}, MatchEnclosed},
		{"web/./a.test.ts", "suite > case", []string{"e"}, MatchExact},
		{"web/other.test.ts", "", nil, ""},
	}
	for _, tc := range tests {
		t.Run(tc.path+"#"+tc.symbol, func(t *testing.T) {
			got, match := s.Match(tc.path, tc.symbol)
			var ids []string
			for _, r := range got {
				ids = append(ids, r.ID)
			}
			if !reflect.DeepEqual(ids, tc.ids) || match != tc.match {
				t.Errorf("got %v %q, want %v %q", ids, match, tc.ids, tc.match)
			}
		})
	}
}

func TestMatchRequirementAggregates(t *testing.T) {
	edge := func(p, sym string) core.TraceEdge {
		return core.TraceEdge{Role: core.TraceRoleTest, Path: p, Symbol: sym, Sources: []core.TraceSource{core.TraceSourceMarker}}
	}
	results := []TestResult{
		{ID: "p1", Path: "a_test.go", Symbol: "TestPass", Result: OutcomePass, Commit: "c1"},
		{ID: "p2", Path: "b_test.go", Symbol: "TestPass", Result: OutcomePass, Commit: "c2"},
		{ID: "f1", Path: "a_test.go", Symbol: "TestFail", Result: OutcomeFail, Commit: "c1"},
		{ID: "s1", Path: "a_test.go", Symbol: "TestSkip", Result: OutcomeSkip},
	}
	s := NewResultSet(results)
	tests := []struct {
		name    string
		edges   []core.TraceEdge
		want    RequirementOutcome
		commits []string
	}{
		{"no linked tests", nil, RequirementUntested, nil},
		{"all linked tests pass", []core.TraceEdge{edge("a_test.go", "TestPass"), edge("b_test.go", "TestPass")}, RequirementPass, []string{"c1", "c2"}},
		{"failing beats passing", []core.TraceEdge{edge("a_test.go", "TestPass"), edge("a_test.go", "TestFail")}, RequirementFail, []string{"c1"}},
		{"a missing test makes it partial", []core.TraceEdge{edge("a_test.go", "TestPass"), edge("a_test.go", "TestGone")}, RequirementPartial, []string{"c1"}},
		{"a skipped test makes it partial", []core.TraceEdge{edge("a_test.go", "TestPass"), edge("a_test.go", "TestSkip")}, RequirementPartial, []string{"c1"}},
		{"only skipped or missing is untested", []core.TraceEdge{edge("a_test.go", "TestSkip"), edge("c_test.go", "")}, RequirementUntested, nil},
		{"a whole-file ref takes every test of the file", []core.TraceEdge{edge("a_test.go", "")}, RequirementFail, []string{"c1"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := s.MatchRequirement(core.TracedRequirement{Tests: tc.edges})
			if got.Result != tc.want || !reflect.DeepEqual(got.Commits, tc.commits) || len(got.Tests) != len(tc.edges) {
				t.Errorf("got %+v, want %s %v", got, tc.want, tc.commits)
			}
		})
	}
}

// TestMatchRequirementsOverTheGraph ingests a go test report against the
// graph fixture: R1 is verified by a Verifies: marker inside the sub-test
// and a trace.tests entry naming it, R3 by a marker in a Vitest it().
func TestMatchRequirementsOverTheGraph(t *testing.T) {
	g, root, _ := fixtureGraph(t)
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/acme\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	report := strings.Join([]string{
		`{"Action":"pass","Package":"example.com/acme/src","Test":"TestNextID/stale_counter","Elapsed":0.01}`,
		`{"Action":"pass","Package":"example.com/acme/src","Test":"TestNextID/fresh","Elapsed":0.01}`,
		`{"Action":"pass","Package":"example.com/acme/src","Test":"TestNextID","Elapsed":0.02}`,
	}, "\n")
	_, raws, err := ParseReport(strings.NewReader(report), "")
	if err != nil {
		t.Fatal(err)
	}
	r := NewTestResolver(os.DirFS(root), root, "")
	results, rep := Stamp(r, raws, "abc123", time.Date(2026, 9, 24, 12, 0, 0, 0, time.FixedZone("CEST", 7200)))
	if rep.Tests != 3 || rep.Passed != 3 || rep.Unmapped != 0 {
		t.Fatalf("report = %+v", rep)
	}
	if results[0].At.Location() != time.UTC || results[0].Commit != "abc123" {
		t.Errorf("stamp = %v %q", results[0].At, results[0].Commit)
	}
	vitest := `{"testResults":[{"name":"` + filepath.ToSlash(filepath.Join(root, "web", "alloc.test.ts")) +
		`","assertionResults":[{"ancestorTitles":["alloc"],"title":"allocates","status":"failed"}]}]}`
	_, vraws, err := ParseReport(strings.NewReader(vitest), "")
	if err != nil {
		t.Fatal(err)
	}
	vres, _ := Stamp(r, vraws, "abc123", time.Now())
	results = append(results, vres...)

	got := map[string]RequirementResult{}
	for _, rr := range MatchRequirements(g, results) {
		got[rr.Ref.String()] = rr
	}
	r1 := got["ACME-SP-0001.R1"]
	if r1.Result != RequirementPass || len(r1.Tests) != 1 ||
		r1.Tests[0].Test != "src/alloc_test.go#TestNextID/stale_counter" || r1.Tests[0].Match != MatchExact ||
		!reflect.DeepEqual(r1.Tests[0].Sources, []core.TraceSource{core.TraceSourceMarker, core.TraceSourceEntry}) {
		t.Errorf("R1 = %+v", r1)
	}
	if r3 := got["ACME-SP-0001.R3"]; r3.Result != RequirementFail || r3.Tests[0].Test != "web/alloc.test.ts#alloc > allocates" {
		t.Errorf("R3 = %+v", r3)
	}
	if r2 := got["ACME-SP-0001.R2"]; r2.Result != RequirementUntested || len(r2.Tests) != 0 {
		t.Errorf("R2 = %+v", r2)
	}
}

func TestResultStore(t *testing.T) {
	dir := t.TempDir()
	p := DefaultResultCachePath(dir, "/some/repo")
	if filepath.Dir(p) != filepath.Join(dir, "test-results") || filepath.Ext(p) != ".json" {
		t.Fatalf("cache path = %s", p)
	}
	if p == DefaultResultCachePath(dir, "/other/repo") {
		t.Fatal("two repositories share a cache file")
	}
	s := NewResultStore(p)

	t.Run("a missing cache is empty", func(t *testing.T) {
		got, corrupt, err := s.Load()
		if err != nil || corrupt || len(got) != 0 {
			t.Fatalf("got %v %v %v", got, corrupt, err)
		}
	})
	at := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	first := []TestResult{
		{ID: "p#TestA", Format: FormatGoTest, Path: "a_test.go", Symbol: "TestA", Result: OutcomeFail, At: at},
		{ID: "p#TestB", Format: FormatGoTest, Path: "a_test.go", Symbol: "TestB", Result: OutcomePass, At: at},
		{ID: "x#TestGhost", Format: FormatGoTest, Symbol: "TestGhost", Result: OutcomePass, At: at},
	}
	t.Run("merge adds", func(t *testing.T) {
		st, err := s.Merge("/some/repo", first)
		if err != nil || st != (MergeStats{Added: 3, Total: 3}) {
			t.Fatalf("stats = %+v, %v", st, err)
		}
	})
	t.Run("the last result wins across formats", func(t *testing.T) {
		later := []TestResult{
			{ID: "p#TestA", Format: FormatJUnit, Path: "a_test.go", Symbol: "TestA", Result: OutcomePass, At: at.Add(time.Hour)},
			{ID: "x#TestGhost", Format: FormatJUnit, Symbol: "TestGhost", Result: OutcomeFail, At: at.Add(time.Hour)},
		}
		st, err := s.Merge("/some/repo", later)
		if err != nil || st != (MergeStats{Added: 1, Replaced: 1, Total: 4}) {
			t.Fatalf("stats = %+v, %v", st, err)
		}
		got, _, err := s.Load()
		if err != nil {
			t.Fatal(err)
		}
		var rows []string
		for _, r := range got {
			rows = append(rows, string(r.Format)+" "+resultRow(r))
		}
		want := []string{
			"junit pass a_test.go#TestA",
			"go pass a_test.go#TestB",
			"go pass unmapped x#TestGhost",
			"junit fail unmapped x#TestGhost",
		}
		if !reflect.DeepEqual(rows, want) {
			t.Errorf("rows = %q, want %q", rows, want)
		}
	})
	for _, corrupt := range []string{"{not json", `{"version":99,"results":[]}`} {
		t.Run("a corrupt cache is rebuilt: "+corrupt, func(t *testing.T) {
			if err := os.WriteFile(p, []byte(corrupt), 0o644); err != nil {
				t.Fatal(err)
			}
			got, isCorrupt, err := s.Load()
			if err != nil || !isCorrupt || len(got) != 0 {
				t.Fatalf("load = %v %v %v", got, isCorrupt, err)
			}
			st, err := s.Merge("/some/repo", first[:1])
			if err != nil || st != (MergeStats{Added: 1, Total: 1, Rebuilt: true}) {
				t.Fatalf("stats = %+v, %v", st, err)
			}
		})
	}
}

func TestFindRoot(t *testing.T) {
	root := t.TempDir()
	deep := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	if got, err := FindRoot(deep); err != nil || got != deep {
		t.Errorf("no repository: got %s, %v", got, err)
	}
	if err := os.Mkdir(filepath.Join(root, ".jj"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got, err := FindRoot(deep); err != nil || got != root {
		t.Errorf("got %s, %v, want %s", got, err, root)
	}
}
