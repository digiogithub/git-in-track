package trace

import (
	"context"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/digiogithub/git-in-track/internal/core"
)

// writeTree creates files under root from a path -> content map.
func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for p, content := range files {
		abs := filepath.Join(root, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func markerLines(ms []Marker) []string {
	out := make([]string, 0, len(ms))
	for _, m := range ms {
		out = append(out, m.Path+":"+strconv.Itoa(m.Line)+" "+m.Ref.String())
	}
	return out
}

const goMarker = "package a\n\n// Implements: GIT-SP-0001.R1\nfunc F() {}\n"

func TestCacheRebuildHonoursIgnores(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		".gitignore":                         "/gen/\n*.gen.go\nweb/dist/*\n!web/dist/keep.js\n",
		"src/a.go":                           goMarker,
		"src/b.gen.go":                       goMarker, // ignored by pattern
		"gen/c.go":                           goMarker, // ignored directory
		"sub/.gitignore":                     "local.py\n",
		"sub/local.py":                       "# Implements: GIT-SP-0001.R2\n", // nested .gitignore
		"sub/kept.py":                        "# Implements: GIT-SP-0001.R3\n",
		"other/local.py":                     "# Implements: GIT-SP-0001.R4\n", // nested rule does not leak
		"node_modules/x/index.js":            "// Implements: GIT-SP-0001.R9\n",
		"vendor/y/y.go":                      goMarker,
		"docs/.pmngr/specs/GIT-SP-0001-a.md": "<!-- Implements: GIT-SP-0001.R9 -->\n",
		"web/dist/app.js":                    "// Implements: GIT-SP-0001.R9\n",
		"web/dist/keep.js":                   "// Implements: GIT-SP-0001.R9\n", // web/dist is always skipped
		".git/info/exclude":                  "excluded.sh\n",
		"excluded.sh":                        "# Implements: GIT-SP-0001.R9\n",
		"notes.txt":                          "// Implements: GIT-SP-0001.R9\n",
		"skipme/z.go":                        goMarker,
		"docs/guide.md":                      "<!-- Verifies: GIT-SP-0001.R5 -->\n<!-- Verifies: GIT-SP-0001 -->\n",
		"bin.go":                             "// Implements: GIT-SP-0001.R9\x00\n",
	})
	c, err := Scan(context.Background(), root, Options{Exclude: []string{"skipme"}})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"docs/guide.md:1 GIT-SP-0001.R5",
		"other/local.py:1 GIT-SP-0001.R4",
		"src/a.go:3 GIT-SP-0001.R1",
		"sub/kept.py:1 GIT-SP-0001.R3",
	}
	if got := markerLines(c.Markers()); !reflect.DeepEqual(got, want) {
		t.Errorf("Markers() = %q\nwant %q", got, want)
	}
	if f := c.Findings(); len(f) != 1 || f[0].Code != CodeMarkerSyntax || f[0].Path != "docs/guide.md" || f[0].Line != 2 {
		t.Errorf("Findings() = %+v", f)
	}
	if got := c.Files(); !reflect.DeepEqual(got, []string{"docs/guide.md", "other/local.py", "src/a.go", "sub/kept.py"}) {
		t.Errorf("Files() = %v", got)
	}
	if got := c.File("./src/a.go"); len(got.Markers) != 1 || got.Markers[0].Symbol != "F" {
		t.Errorf("File(src/a.go) = %+v", got)
	}
}

func TestCacheUpdate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		".gitignore": "ignored/\n",
		"a.go":       goMarker,
		"b.go":       "package a\n",
	})
	c, err := Scan(ctx, root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	// b.go gains a marker, a.go is deleted, an ignored file and a file of an
	// unscanned type change: only the first two matter.
	writeTree(t, root, map[string]string{
		"b.go":         "package a\n\n// Verifies: GIT-SP-0001.R2\nfunc TestB() {}\n",
		"ignored/x.go": goMarker,
		"n.txt":        "// Implements: GIT-SP-0001.R9\n",
	})
	if err := os.Remove(filepath.Join(root, "a.go")); err != nil {
		t.Fatal(err)
	}
	if err := c.Update(ctx, []string{"a.go", "b.go", "ignored/x.go", "n.txt", "missing/z.go", "../escape.go"}); err != nil {
		t.Fatal(err)
	}
	if got, want := markerLines(c.Markers()), []string{"b.go:3 GIT-SP-0001.R2"}; !reflect.DeepEqual(got, want) {
		t.Errorf("after Update: %q, want %q", got, want)
	}
	// A changed .gitignore rebuilds everything: un-ignoring picks up x.go.
	writeTree(t, root, map[string]string{".gitignore": "\n"})
	if err := c.Update(ctx, []string{".gitignore"}); err != nil {
		t.Fatal(err)
	}
	want := []string{"b.go:3 GIT-SP-0001.R2", "ignored/x.go:3 GIT-SP-0001.R1"}
	if got := markerLines(c.Markers()); !reflect.DeepEqual(got, want) {
		t.Errorf("after .gitignore change: %q, want %q", got, want)
	}
	if got := markerLines(c.MarkersFor(core.RequirementRef{Spec: "GIT-SP-0001", Number: 1})); !reflect.DeepEqual(got, want[1:]) {
		t.Errorf("MarkersFor(R1) = %q", got)
	}
}

func TestCacheCancelled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	root := t.TempDir()
	writeTree(t, root, map[string]string{"a.go": goMarker})
	if _, err := Scan(ctx, root, Options{}); err == nil {
		t.Error("Scan() with a cancelled context succeeded")
	}
	if _, err := Scan(context.Background(), filepath.Join(root, "absent"), Options{}); err == nil {
		t.Error("Scan() of a missing root succeeded")
	}
}

// TestNotImportedByCoreOrVault keeps the scanner out of the WASM build: the
// packages compiled to WebAssembly must never import it (R-MARK-4).
func TestNotImportedByCoreOrVault(t *testing.T) {
	t.Parallel()
	const self = "github.com/digiogithub/git-in-track/internal/trace"
	for _, dir := range []string{"../core", "../vault"} {
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
