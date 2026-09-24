package core

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestBodyStartLine(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		data string
		want int
	}{
		{"body after one blank line", "---\nid: A\n---\n\nbody\n", 5},
		{"body right after the fence", "---\nid: A\n---\nbody\n", 4},
		{"several blank lines", "---\nid: A\ntype: spec\n---\n\n\n\nbody\n", 8},
		{"crlf and bom", "\ufeff---\r\nid: A\r\n---\r\n\r\nbody\r\n", 5},
		{"empty front matter", "---\n---\n\nbody\n", 4},
		{"no body", "---\nid: A\n---\n", 0},
		{"not closed", "---\nid: A\n", 0},
		{"no front matter", "body\n", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := BodyStartLine([]byte(tt.data)); got != tt.want {
				t.Errorf("BodyStartLine(%q) = %d, want %d", tt.data, got, tt.want)
			}
		})
	}
}

// specLinesFixture parses a spec whose front matter spans many lines.
func specLinesFixture(t *testing.T) ([]byte, *Item) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "spec-lines-item.md"))
	if err != nil {
		t.Fatal(err)
	}
	it, err := ParseItem("docs/.pmngr/specs/TEST-SP-0007-requirement-diagnostics-on-file-lines.md", data)
	if err != nil {
		t.Fatalf("ParseItem(): %v", err)
	}
	return data, it
}

// TestRequirementDiagnosticsFileLinesGolden pins the file line of every
// requirement diagnostic of a spec with a multi-line front matter
// (GIT-US-0144), and the text of that line, so a reviewer of the golden sees
// what each finding points at.
func TestRequirementDiagnosticsFileLinesGolden(t *testing.T) {
	t.Parallel()
	data, it := specLinesFixture(t)
	lines := strings.Split(string(data), "\n")

	var b strings.Builder
	b.WriteString("# RequirementDiagnostics(spec-lines-item.md): file line, then the text on it\n")
	for _, d := range RequirementDiagnostics(it, specConfig(t)) {
		if d.Line < 1 || d.Line > len(lines) {
			t.Errorf("%s %s: line %d is not a line of the file", d.Code, d.Field, d.Line)
			continue
		}
		if strings.Contains(d.Message, "of the body") {
			t.Errorf("%s %s: message still counts body lines: %s", d.Code, d.Field, d.Message)
		}
		fmt.Fprintf(&b, "%s %s %s: %s\n    %d | %s\n", d.Code, d.Severity, d.Field, d.Message, d.Line, lines[d.Line-1])
	}
	compareGolden(t, "spec-lines-diagnostics.txt", []byte(b.String()))
}

// TestRequirementDiagnosticsLinesOfAnItemInMemory checks that an item with no
// file behind it is reported on the lines of the file it would be written as.
func TestRequirementDiagnosticsLinesOfAnItemInMemory(t *testing.T) {
	t.Parallel()
	_, parsed := specLinesFixture(t)
	canonical, err := SerializeItem(parsed)
	if err != nil {
		t.Fatal(err)
	}
	fromFile, err := ParseItem(parsed.Path, canonical)
	if err != nil {
		t.Fatal(err)
	}
	inMemory := cloneItem(fromFile)
	inMemory.BodyLine, inMemory.reqLines = 0, nil
	withBlank := cloneItem(&inMemory)
	withBlank.Body = "\n\n" + withBlank.Body

	cfg := specConfig(t)
	want := RequirementDiagnostics(fromFile, cfg)
	if got := RequirementDiagnostics(&inMemory, cfg); !reflect.DeepEqual(got, want) {
		t.Errorf("in memory:\n%v\nwant the diagnostics of the written file:\n%v", got, want)
	}
	// The writer trims the blank lines a body opens with; the lines stay
	// those of the written file.
	if got := RequirementDiagnostics(&withBlank, cfg); !reflect.DeepEqual(got, want) {
		t.Errorf("body with leading blank lines:\n%v\nwant:\n%v", got, want)
	}
	if reflect.DeepEqual(want, RequirementDiagnostics(parsed, cfg)) {
		t.Error("the fixture is canonical already: the test no longer tells the layouts apart")
	}
}
