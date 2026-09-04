package output

import (
	"bytes"
	"strings"
	"testing"
)

func TestTableAlignment(t *testing.T) {
	t.Parallel()

	var b bytes.Buffer
	err := Table(&b, []string{"ID", "TYPE", "TITLE"}, [][]string{
		{"DEMO-US-0001", "story", "Guest checkout"},
		{"DEMO-T-1", "task", "Add address validation"},
	})
	if err != nil {
		t.Fatalf("table: %v", err)
	}
	want := "" +
		"ID            TYPE   TITLE\n" +
		"DEMO-US-0001  story  Guest checkout\n" +
		"DEMO-T-1      task   Add address validation\n"
	if b.String() != want {
		t.Errorf("table =\n%q\nwant\n%q", b.String(), want)
	}
}

func TestTableAlignsOnRuneWidth(t *testing.T) {
	t.Parallel()

	var b bytes.Buffer
	if err := Table(&b, []string{"NAME", "ROLE"}, [][]string{
		{"café", "team"},
		{"a", "project"},
	}); err != nil {
		t.Fatalf("table: %v", err)
	}
	lines := strings.Split(strings.TrimRight(b.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("lines = %q", lines)
	}
	for _, line := range lines {
		if got := strings.Index(line, strings.Fields(line)[len(strings.Fields(line))-1]); got == 0 {
			t.Fatalf("line %q has no second column", line)
		}
	}
	if !strings.HasPrefix(lines[1], "café  team") {
		t.Errorf("multi-byte runes are counted as bytes: %q", lines[1])
	}
}

func TestTableWithoutRows(t *testing.T) {
	t.Parallel()

	var b bytes.Buffer
	if err := Table(&b, []string{"ID", "PATH"}, nil); err != nil {
		t.Fatalf("table: %v", err)
	}
	if b.String() != "ID  PATH\n" {
		t.Errorf("table = %q", b.String())
	}
}

func TestTableHasNoTrailingSpaces(t *testing.T) {
	t.Parallel()

	var b bytes.Buffer
	if err := Table(&b, []string{"ID", "KEY", "ITEMS"}, [][]string{
		{"acme-api", "", ""},
		{"acme-web", "AWEB", "17"},
	}); err != nil {
		t.Fatalf("table: %v", err)
	}
	for _, line := range strings.Split(strings.TrimRight(b.String(), "\n"), "\n") {
		if strings.TrimRight(line, " ") != line {
			t.Errorf("line %q ends in spaces", line)
		}
	}
}

func TestJSONIsIndentedAndDeterministic(t *testing.T) {
	t.Parallel()

	payload := map[string]any{"zeta": 1, "alpha": []string{"b", "a"}, "title": "Tom & Jerry"}
	var first, second bytes.Buffer
	if err := JSON(&first, payload); err != nil {
		t.Fatalf("json: %v", err)
	}
	for range 5 {
		second.Reset()
		if err := JSON(&second, payload); err != nil {
			t.Fatalf("json: %v", err)
		}
		if first.String() != second.String() {
			t.Fatalf("two encodings differ:\n%s\n%s", first.String(), second.String())
		}
	}
	got := first.String()
	if !strings.Contains(got, "\n  \"alpha\"") {
		t.Errorf("output is not indented by two spaces:\n%s", got)
	}
	if !strings.Contains(got, "Tom & Jerry") {
		t.Errorf("HTML escaping mangled the payload:\n%s", got)
	}
	if strings.Index(got, `"alpha"`) > strings.Index(got, `"zeta"`) {
		t.Errorf("map keys are not sorted:\n%s", got)
	}
	if !strings.HasSuffix(got, "}\n") {
		t.Errorf("output does not end in a newline:\n%q", got)
	}
}

func TestPrinterTextMode(t *testing.T) {
	t.Parallel()

	var out, errs bytes.Buffer
	p := New(&out, &errs, false)
	if p.JSONMode() {
		t.Error("JSONMode is on in text mode")
	}
	if err := p.Table([]string{"A"}, [][]string{{"1"}}); err != nil {
		t.Fatalf("table: %v", err)
	}
	if err := p.JSON(map[string]string{"a": "1"}); err != nil {
		t.Fatalf("json: %v", err)
	}
	p.Line("hello")
	if got := out.String(); got != "A\n1\nhello\n" {
		t.Errorf("stdout = %q", got)
	}
	if errs.Len() != 0 {
		t.Errorf("stderr = %q, want it empty", errs.String())
	}
}

func TestPrinterJSONMode(t *testing.T) {
	t.Parallel()

	var out, errs bytes.Buffer
	p := New(&out, &errs, true)
	if err := p.Table([]string{"A"}, [][]string{{"1"}}); err != nil {
		t.Fatalf("table: %v", err)
	}
	if err := p.JSON(map[string]string{"a": "1"}); err != nil {
		t.Fatalf("json: %v", err)
	}
	p.Line("indexing…")
	if got := out.String(); got != "{\n  \"a\": \"1\"\n}\n" {
		t.Errorf("stdout = %q, want only the payload", got)
	}
	if got := errs.String(); got != "indexing…\n" {
		t.Errorf("stderr = %q, want the human line", got)
	}
}

func TestPrinterQuiet(t *testing.T) {
	t.Parallel()

	var out, errs bytes.Buffer
	p := New(&out, &errs, false)
	p.SetQuiet(true)
	p.Line("hello")
	if err := p.Table([]string{"A"}, [][]string{{"1"}}); err != nil {
		t.Fatalf("table: %v", err)
	}
	p.Warnf("careful\n")
	if out.Len() != 0 {
		t.Errorf("stdout = %q, want it empty", out.String())
	}
	if errs.String() != "careful\n" {
		t.Errorf("stderr = %q, want warnings to survive --quiet", errs.String())
	}
}
