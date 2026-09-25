package trace

import (
	"reflect"
	"strings"
	"testing"
)

// refsOf renders the refs of a match for comparison.
func refsOf(refs []markerRef) []string {
	out := make([]string, 0, len(refs))
	for _, r := range refs {
		s := r.Ref.String()
		if r.Project != "" {
			s = string(r.Project) + "/" + s
		}
		out = append(out, s)
	}
	return out
}

func TestMatchLine(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		line string
		syn  syntax
		open block
		want lineResult
		kind Kind
		refs []string
	}{
		// "//" and "/* */"
		{"slash line", "// Implements: GIT-SP-0003.R2", synSlash, blockNone, lineMarker, KindImplements, []string{"GIT-SP-0003.R2"}},
		{"slash indented, no space after opener", "\t\t//Verifies: GIT-SP-0003.R2", synSlash, blockNone, lineMarker, KindVerifies, []string{"GIT-SP-0003.R2"}},
		{"slash several refs", "// Verifies: GIT-SP-0003.R2, GIT-SP-0003.R4 ,GIT-SP-0010.R12", synSlash, blockNone, lineMarker, KindVerifies,
			[]string{"GIT-SP-0003.R2", "GIT-SP-0003.R4", "GIT-SP-0010.R12"}},
		{"slash trailing comma", "// Implements: GIT-SP-0003.R2,  ", synSlash, blockNone, lineMarker, KindImplements, []string{"GIT-SP-0003.R2"}},
		{"slash qualified", "// Implements: WEB/WEB-SP-0001.R1", synSlash, blockNone, lineMarker, KindImplements, []string{"WEB/WEB-SP-0001.R1"}},
		{"block one line", "/* Implements: GIT-SP-0003.R2 */", synSlash, blockNone, lineMarker, KindImplements, []string{"GIT-SP-0003.R2"}},
		{"javadoc one line", "/** Verifies: GIT-SP-0003.R2*/", synSlash, blockNone, lineMarker, KindVerifies, []string{"GIT-SP-0003.R2"}},
		{"block continuation star", " * Implements: WEB/WEB-SP-0001.R1", synSlash, blockC, lineMarker, KindImplements, []string{"WEB/WEB-SP-0001.R1"}},
		{"block continuation bare", "Implements: GIT-SP-0003.R2 */", synSlash, blockC, lineMarker, KindImplements, []string{"GIT-SP-0003.R2"}},
		{"star outside a block", " * Implements: GIT-SP-0003.R2", synSlash, blockNone, lineNone, "", nil},
		{"bare outside a block", "Implements: GIT-SP-0003.R2", synSlash, blockNone, lineNone, "", nil},
		{"trailing comment after code", "x := 1 // Implements: GIT-SP-0003.R2", synSlash, blockNone, lineNone, "", nil},
		{"code after block closer", "/* Implements: GIT-SP-0003.R2 */ x()", synSlash, blockNone, lineMalformed, KindImplements, nil},
		{"closer after a line comment", "// Implements: GIT-SP-0003.R2 */", synSlash, blockNone, lineMalformed, KindImplements, nil},
		{"hash in a slash file", "# Implements: GIT-SP-0003.R2", synSlash, blockNone, lineNone, "", nil},
		// "#"
		{"hash", "# Implements: GIT-SP-0003.R2", synHash, blockNone, lineMarker, KindImplements, []string{"GIT-SP-0003.R2"}},
		{"hash indented", "    #   Verifies: GIT-SP-0003.R2, GIT-SP-0003.R3", synHash, blockNone, lineMarker, KindVerifies, []string{"GIT-SP-0003.R2", "GIT-SP-0003.R3"}},
		{"slash in a hash file", "// Implements: GIT-SP-0003.R2", synHash, blockNone, lineNone, "", nil},
		// "--"
		{"dash", "-- Implements: GIT-SP-0007.R1", synDash, blockNone, lineMarker, KindImplements, []string{"GIT-SP-0007.R1"}},
		{"dash trailing after sql", "SELECT 1; -- Implements: GIT-SP-0007.R1", synDash, blockNone, lineNone, "", nil},
		// "<!-- -->"
		{"html one line", "<!-- Verifies: GIT-SP-0003.R2 -->", synHTML, blockNone, lineMarker, KindVerifies, []string{"GIT-SP-0003.R2"}},
		{"html no spaces", "<!--Implements: GIT-SP-0003.R2-->", synHTML, blockNone, lineMarker, KindImplements, []string{"GIT-SP-0003.R2"}},
		{"html continuation", "  Implements: GIT-SP-0003.R2", synHTML, blockHTML, lineMarker, KindImplements, []string{"GIT-SP-0003.R2"}},
		{"html wrong closer", "<!-- Implements: GIT-SP-0003.R2 */", synHTML, blockNone, lineMalformed, KindImplements, nil},
		{"hash heading in html", "# Implements: GIT-SP-0003.R2", synHTML, blockNone, lineNone, "", nil},
		{"vue uses both rows", "// Implements: GIT-SP-0003.R2", synSlash | synHTML, blockNone, lineMarker, KindImplements, []string{"GIT-SP-0003.R2"}},
		// keyword spelling: not markers, and not findings either
		{"lower-case keyword", "// implements: GIT-SP-0003.R2", synSlash, blockNone, lineNone, "", nil},
		{"space before colon", "// Implements : GIT-SP-0003.R2", synSlash, blockNone, lineNone, "", nil},
		{"other word", "// Implemented: GIT-SP-0003.R2", synSlash, blockNone, lineNone, "", nil},
		{"prose", "// Implements the parser of GIT-SP-0003.R2", synSlash, blockNone, lineNone, "", nil},
		// ref list: W-MARKER-SYNTAX
		{"bare spec id", "// Implements: GIT-SP-0003", synSlash, blockNone, lineMalformed, KindImplements, nil},
		{"padded number", "// Implements: GIT-SP-0003.R02", synSlash, blockNone, lineMalformed, KindImplements, nil},
		{"lower-case r", "// Implements: GIT-SP-0003.r2", synSlash, blockNone, lineMalformed, KindImplements, nil},
		{"no space after colon", "// Implements:GIT-SP-0003.R2", synSlash, blockNone, lineMalformed, KindImplements, nil},
		{"empty list", "// Verifies:", synSlash, blockNone, lineMalformed, KindVerifies, nil},
		{"only a comma", "// Verifies: ,", synSlash, blockNone, lineMalformed, KindVerifies, nil},
		{"double comma", "// Verifies: GIT-SP-0003.R2,, GIT-SP-0003.R3", synSlash, blockNone, lineMalformed, KindVerifies, nil},
		{"trailing prose", "// Implements: GIT-SP-0003.R2 and more", synSlash, blockNone, lineMalformed, KindImplements, nil},
		{"second keyword", "// Implements: GIT-SP-0003.R2 Verifies: GIT-SP-0003.R3", synSlash, blockNone, lineMalformed, KindImplements, nil},
		{"space separated", "// Implements: GIT-SP-0003.R2 GIT-SP-0003.R3", synSlash, blockNone, lineMalformed, KindImplements, nil},
		{"not a spec", "// Implements: GIT-US-0003.R2", synSlash, blockNone, lineMalformed, KindImplements, nil},
		{"bad qualifier", "// Implements: web/WEB-SP-0001.R1", synSlash, blockNone, lineMalformed, KindImplements, nil},
		{"two qualifiers", "// Implements: A1/B1/WEB-SP-0001.R1", synSlash, blockNone, lineMalformed, KindImplements, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			kind, refs, res := matchLine(tt.line, tt.syn, tt.open)
			if res != tt.want {
				t.Fatalf("matchLine(%q) result = %d, want %d", tt.line, res, tt.want)
			}
			if kind != tt.kind {
				t.Errorf("kind = %q, want %q", kind, tt.kind)
			}
			if got := refsOf(refs); len(tt.refs) > 0 && !reflect.DeepEqual(got, tt.refs) {
				t.Errorf("refs = %v, want %v", got, tt.refs)
			}
		})
	}
}

func TestTypeOf(t *testing.T) {
	t.Parallel()
	tests := []struct {
		path string
		ok   bool
		syn  syntax
	}{
		{"internal/core/ids.go", true, synSlash},
		{"web/src/App.TSX", true, synSlash},
		{"scripts/build.sh", true, synHash},
		{"analysis/model.R", true, synHash},
		{"Makefile", true, synHash},
		{"deploy/Dockerfile", true, synHash},
		{"db/001_init.sql", true, synDash},
		{"docs/guide.md", true, synHTML},
		{"web/src/Card.vue", true, synSlash | synHTML},
		{"README.txt", false, 0},
		{"go.sum", false, 0},
		{"image.png", false, 0},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()
			ft, ok := typeOf(tt.path)
			if ok != tt.ok || ft.syntax != tt.syn {
				t.Errorf("typeOf(%q) = %v, %v; want %v, %v", tt.path, ft.syntax, ok, tt.syn, tt.ok)
			}
		})
	}
}

func TestNextBlockState(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		line string
		syn  syntax
		open block
		want block
	}{
		{"opens", "/*", synSlash, blockNone, blockC},
		{"opens and closes", "/* x */ y", synSlash, blockNone, blockNone},
		{"closes", " */", synSlash, blockC, blockNone},
		{"stays open", " * text", synSlash, blockC, blockC},
		{"opener in a string", `s := "/*"`, synSlash, blockNone, blockNone},
		{"opener after a line comment", "// see /*", synSlash, blockNone, blockNone},
		{"html opens", "<!--", synHTML, blockNone, blockHTML},
		{"html closes", "-->", synHTML, blockHTML, blockNone},
		{"hash files have no blocks", "/*", synHash, blockNone, blockNone},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := nextBlockState(tt.line, tt.syn, tt.open); got != tt.want {
				t.Errorf("nextBlockState(%q) = %d, want %d", strings.TrimSpace(tt.line), got, tt.want)
			}
		})
	}
}
