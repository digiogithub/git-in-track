package trace

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// render prints a result one hit per line: "<line> <kind> <target> [<symbol>]"
// for markers, "<line> <code>" for findings.
func render(r FileResult) []string {
	var out []string
	for _, m := range r.Markers {
		out = append(out, fmt.Sprintf("%d %s %s [%s]", m.Line, m.Kind, m.Target(), m.Symbol))
	}
	for _, f := range r.Findings {
		out = append(out, fmt.Sprintf("%d %s", f.Line, f.Code))
	}
	return out
}

// TestScanFileFixtures scans the fixture files of testdata. They carry a .txt
// suffix so that a scan of this repository never reads them; the test scans
// each under the path without it.
func TestScanFileFixtures(t *testing.T) {
	t.Parallel()
	tests := []struct {
		fixture string
		want    []string
	}{
		{"sample.go.txt", []string{
			"3 implements GIT-SP-0001.R1 []",                        // package doc: whole file
			"9 implements GIT-SP-0003.R2 [Allocator]",               // doc comment of a type
			"9 implements GIT-SP-0003.R4 [Allocator]",               // several refs on one line
			"12 implements GIT-SP-0003.R5 []",                       // detached by a blank line
			"16 implements GIT-SP-0003.R6 [Allocator.NextID]",       // inside a method body
			"26 implements GIT-SP-0003.R7 [limit]",                  // doc of one spec of a group
			"32 verifies GIT-SP-0003.R8 [TestNextID]",               // block doc comment
			"36 verifies GIT-SP-0003.R2 [TestNextID/stale_counter]", // inside a t.Run sub-test
			"39 verifies GIT-SP-0003.R9 [TestNextID]",               // one-line block comment
			"42 W-MARKER-SYNTAX",                                    // bare spec ID
			"45 W-MARKER-SYNTAX",                                    // padded number
			"46 W-MARKER-SYNTAX",                                    // trailing prose
		}},
		{"sample.test.ts.txt", []string{
			"3 implements WEB/WEB-SP-0001.R1 [parse]",
			"5 implements WEB-SP-0001.R2 [parse]",
			"10 implements WEB-SP-0001.R3 [render]",
			"17 implements WEB-SP-0001.R4 [Board.move]",
			"20 implements WEB-SP-0001.R5 [Board.move]",
			"25 verifies WEB-SP-0001.R1 [parser > reads a heading]",
			"31 W-MARKER-SYNTAX", // a line comment has no closer
		}},
		{"sample.py.txt", []string{
			"1 implements GIT-SP-0004.R1 []",
			"6 implements GIT-SP-0004.R2 [Store.path]",
			"9 implements GIT-SP-0004.R3 [Store.path]",
			"12 verifies GIT-SP-0004.R4 [test_store]",
		}},
		{"sample.md.txt", []string{
			"3 implements GIT-SP-0005.R1 []",
			"6 verifies GIT-SP-0005.R2 []",
		}},
	}
	for _, tt := range tests {
		t.Run(tt.fixture, func(t *testing.T) {
			t.Parallel()
			src, err := os.ReadFile(filepath.Join("testdata", tt.fixture))
			if err != nil {
				t.Fatal(err)
			}
			p := "pkg/" + strings.TrimSuffix(tt.fixture, ".txt")
			got := ScanFile(p, src)
			if lines := render(got); !reflect.DeepEqual(lines, tt.want) {
				t.Errorf("ScanFile(%s):\n got %s\nwant %s", p, strings.Join(lines, "\n     "), strings.Join(tt.want, "\n     "))
			}
			for _, m := range got.Markers {
				if m.Path != p {
					t.Errorf("marker path = %q, want %q", m.Path, p)
				}
			}
		})
	}
}

func TestScanFileSnippets(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		path string
		src  string
		want []string
	}{
		{"unscanned type", "notes.txt", "// Implements: GIT-SP-0001.R1\n", nil},
		{"no keyword at all", "a.go", "package a\n", nil},
		{"yaml hash", "ci.yml", "# Verifies: GIT-SP-0001.R1\nkey: 1\n", []string{"1 verifies GIT-SP-0001.R1 []"}},
		{"makefile", "Makefile", "# Implements: GIT-SP-0001.R1\nall:\n", []string{"1 implements GIT-SP-0001.R1 []"}},
		{"sql dash", "q.sql", "-- Implements: GIT-SP-0007.R1\nSELECT 1; -- Implements: GIT-SP-0007.R2\n", []string{"1 implements GIT-SP-0007.R1 []"}},
		{"html continuation", "p.html", "<!--\n  Verifies: GIT-SP-0001.R1\n-->\n<p>Implements: GIT-SP-0001.R2</p>\n",
			[]string{"2 verifies GIT-SP-0001.R1 []"}},
		{"markdown hash is a heading", "g.md", "# Implements: GIT-SP-0001.R1\n", nil},
		{"markdown tilde fence", "g.md", "~~~~\n<!-- Implements: GIT-SP-0001.R1 -->\n~~~\n~~~~\n<!-- Implements: GIT-SP-0001.R2 -->\n",
			[]string{"5 implements GIT-SP-0001.R2 []"}},
		{"java block doc", "A.java", "/**\n * Implements: GIT-SP-0001.R1\n */\nclass A {}\n", []string{"2 implements GIT-SP-0001.R1 []"}},
		{"block opened in a string is no block", "a.ts", "const s = \"/*\"\nImplements: GIT-SP-0001.R1\n", nil},
		{"crlf", "a.py", "# Implements: GIT-SP-0001.R1\r\ndef f():\r\n  pass\r\n", []string{"1 implements GIT-SP-0001.R1 [f]"}},
		{"go block comment opened after code, between declarations", "a.go", "package a\n\nvar x = 1 /*\nImplements: GIT-SP-0001.R1\n*/\n",
			[]string{"4 implements GIT-SP-0001.R1 []"}},
		{"go comment in a raw string", "a.go", "package a\n\nconst s = `\n// Verifies: GIT-SP-0001.R1\n`\n", nil},
		{"go file that does not parse", "a.go", "package a\n\n// Implements: GIT-SP-0001.R1\nfunc (\n", []string{"3 implements GIT-SP-0001.R1 []"}},
		{"go generic receiver", "a.go", "package a\n\ntype L[T any] struct{}\n\n// Implements: GIT-SP-0001.R1\nfunc (l *L[T]) Push() {}\n",
			[]string{"5 implements GIT-SP-0001.R1 [L.Push]"}},
		{"js one-line arrow closes", "a.js", "const f = (x) => x + 1;\n// Implements: GIT-SP-0001.R1\n\nfunction g() {}\n",
			[]string{"2 implements GIT-SP-0001.R1 []"}},
		{"js one-line test closes", "a.test.js", "it('a', () => { x() })\n\nit('b', () => {\n  // Verifies: GIT-SP-0001.R1\n})\n",
			[]string{"4 verifies GIT-SP-0001.R1 [b]"}},
		{"js brace on the next line", "a.js", "function f()\n{\n  // Implements: GIT-SP-0001.R1\n}\n",
			[]string{"3 implements GIT-SP-0001.R1 [f]"}},
		{"js decorator between doc and method", "a.ts", "class C {\n  // Implements: GIT-SP-0001.R1\n  @dec()\n  run() {\n  }\n}\n",
			[]string{"2 implements GIT-SP-0001.R1 [C.run]"}},
		{"js template literal", "a.ts", "const s = `\n// Implements: GIT-SP-0001.R1\n`\n", nil},
		{"js after a closed function", "a.ts", "function f() {\n}\n// Implements: GIT-SP-0001.R1\nconst x = 1\n",
			[]string{"3 implements GIT-SP-0001.R1 []"}},
		{"python comment after a body", "a.py", "def f():\n    pass\n\n# Implements: GIT-SP-0001.R1\n\nx = 1\n",
			[]string{"4 implements GIT-SP-0001.R1 []"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := render(ScanFile(tt.path, []byte(tt.src)))
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ScanFile(%s) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestMarkerRendering(t *testing.T) {
	t.Parallel()
	r := ScanFile("a/b.go", []byte("package b\n\n// Implements: WEB/WEB-SP-0001.R3\nfunc F() {}\n"))
	if len(r.Markers) != 1 {
		t.Fatalf("markers = %v", r.Markers)
	}
	m := r.Markers[0]
	if m.Target() != "WEB/WEB-SP-0001.R3" || m.TraceRef() != "a/b.go#F" {
		t.Errorf("Target() = %q, TraceRef() = %q", m.Target(), m.TraceRef())
	}
	m.Symbol = ""
	if m.TraceRef() != "a/b.go" {
		t.Errorf("file-level TraceRef() = %q", m.TraceRef())
	}
}
