package trace

import (
	"reflect"
	"testing"
)

const spanPy = "import os\n\nclass Store:\n    def get(self):\n\n        return 1\n\n" +
	"    # a comment\n    def put(self):\n        pass\n\ndef main():\n    pass\n"

const spanTS = "const x = 1;\n\nexport function build() {\n  return x;\n}\n\n" +
	"describe('store', () => {\n  it('reads', () => {\n    expect(1).toBe(1);\n  });\n});\n"

const spanGo = "package a\n\n// F does it.\nfunc F() {\n\tx := 1\n\t_ = x\n}\n\n" +
	"type T struct{}\n\nfunc (t T) M() {}\n\nfunc TestF(t *testing.T) {\n\tt.Run(\"a b\", func(t *testing.T) {\n" +
	"\t\tt.Run(\"c\", func(t *testing.T) {})\n\t})\n}\n"

func TestSymbolsAt(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, path, src string
		want            map[int]string
	}{
		{"python", "a.py", spanPy, map[int]string{1: "", 3: "Store", 4: "Store.get", 5: "Store.get", 6: "Store.get", 10: "Store.put", 13: "main"}},
		{"typescript", "a.test.ts", spanTS, map[int]string{1: "", 3: "build", 4: "build", 7: "store", 9: "store > reads"}},
		{"go", "a.go", spanGo, map[int]string{1: "", 3: "F", 5: "F", 9: "T", 11: "T.M", 15: "TestF/a_b/c", 16: "TestF/a_b"}},
		{"no declarations", "a.yaml", "a: 1\n", map[int]string{1: ""}},
		{"not scanned", "a.txt", "x\n", map[int]string{1: ""}},
		{"out of range", "a.py", spanPy, map[int]string{99: ""}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var lines []int
			for ln := range tc.want {
				lines = append(lines, ln)
			}
			if got := SymbolsAt(tc.path, []byte(tc.src), lines); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("SymbolsAt() = %v\nwant %v", got, tc.want)
			}
		})
	}
}

func TestDeclaredSymbols(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, path, src string
		want            []string
		ok              bool
	}{
		{"python", "a.py", spanPy, []string{"Store", "Store.get", "Store.put", "main"}, true},
		{"typescript", "a.test.ts", spanTS, []string{"build", "store", "store > reads"}, true},
		{"go", "a.go", spanGo, []string{"F", "T", "T.M", "TestF", "TestF/a_b", "TestF/a_b/c"}, true},
		{"go that does not parse", "a.go", "package a\nfunc {\n", nil, false},
		{"no declarations", "a.sql", "select 1;\n", nil, false},
		{"not scanned", "a.txt", "x\n", nil, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := DeclaredSymbols(tc.path, []byte(tc.src))
			if ok != tc.ok || !reflect.DeepEqual(got, tc.want) {
				t.Errorf("DeclaredSymbols() = %q, %v; want %q, %v", got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestSymbolsOverlap(t *testing.T) {
	t.Parallel()
	tests := []struct {
		a, b string
		want bool
	}{
		{"F", "F", true},
		{"T", "T.M", true},
		{"TestX/sub", "TestX", true},
		{"d > it", "d", true},
		{"F", "FG", false},
		{"TestX/a", "TestX/b", false},
		{"T.M", "T.N", false},
	}
	for _, tc := range tests {
		t.Run(tc.a+"|"+tc.b, func(t *testing.T) {
			t.Parallel()
			if got := symbolsOverlap(tc.a, tc.b); got != tc.want {
				t.Errorf("symbolsOverlap(%q, %q) = %v", tc.a, tc.b, got)
			}
		})
	}
}
