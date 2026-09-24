package core

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseRequirementRef(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in      string
		want    RequirementRef
		wantErr bool
	}{
		{in: "GIT-SP-0003.R2", want: RequirementRef{Spec: "GIT-SP-0003", Number: 2}},
		{in: "ACME-SP-12345.R140", want: RequirementRef{Spec: "ACME-SP-12345", Number: 140}},
		{in: "GIT-SP-0003.R02", wantErr: true},
		{in: "GIT-SP-0003.r2", wantErr: true},
		{in: "GIT-SP-0003.R0", wantErr: true},
		{in: "GIT-SP-0003", wantErr: true},
		{in: "GIT-US-0003.R2", wantErr: true},
		{in: "WEB/WEB-SP-0001.R1", wantErr: true},
		{in: "GIT-SP-003.R2", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()
			got, err := ParseRequirementRef(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseRequirementRef(%q) = %v, want an error", tt.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseRequirementRef(%q): %v", tt.in, err)
			}
			if got != tt.want {
				t.Fatalf("ParseRequirementRef(%q) = %+v, want %+v", tt.in, got, tt.want)
			}
			if got.String() != tt.in {
				t.Errorf("String() = %q, want %q", got.String(), tt.in)
			}
		})
	}
}

func TestRequirementRefAnchorAndKey(t *testing.T) {
	t.Parallel()

	ref := RequirementRef{Spec: "GIT-SP-0003", Number: 2}
	if got := ref.Anchor(); got != "git-sp-0003-r2" {
		t.Errorf("Anchor() = %q", got)
	}
	if got := ref.Key(); got != "R2" {
		t.Errorf("Key() = %q", got)
	}
	for key, want := range map[string]bool{"R1": true, "R10": true, "R02": false, "r2": false, "R0": false, "S1": false} {
		if _, ok := ParseRequirementKey(key); ok != want {
			t.Errorf("ParseRequirementKey(%q) ok = %v, want %v", key, ok, want)
		}
	}
}

func TestSpecTypeCode(t *testing.T) {
	t.Parallel()

	key, code, n, err := ParseItemID("GIT-SP-0003")
	if err != nil || key != "GIT" || code != CodeSpec || n != 3 {
		t.Fatalf("ParseItemID = %q %q %d %v", key, code, n, err)
	}
	if typ, ok := ItemTypeFor(CodeSpec); !ok || typ != TypeSpec {
		t.Errorf("ItemTypeFor(SP) = %q %v", typ, ok)
	}
	if c, ok := TypeCodeFor(TypeSpec); !ok || c != CodeSpec {
		t.Errorf("TypeCodeFor(spec) = %q %v", c, ok)
	}
	if dir, ok := ItemDirName(TypeSpec); !ok || dir != "specs" {
		t.Errorf("ItemDirName(spec) = %q %v", dir, ok)
	}
	if got := IDFromFileName("specs/GIT-SP-0003-item-id-allocation.md"); got != "GIT-SP-0003" {
		t.Errorf("IDFromFileName = %q", got)
	}
}

// specFixture reads the spec fixture of testdata/.
func specFixture(t *testing.T) *Item {
	t.Helper()
	src, err := os.ReadFile(filepath.Join("testdata", "spec-item.md"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	it, err := ParseItem("specs/ACME-SP-0003-item-id-allocation.md", src)
	if err != nil {
		t.Fatalf("ParseItem(): %v", err)
	}
	return it
}

func TestParseSpecBodyGolden(t *testing.T) {
	t.Parallel()

	it := specFixture(t)
	body := ParseSpecBody(it.ID, it.Body)
	for _, blk := range body.Blocks {
		if it.Body[blk.Start:blk.End] != blk.Text {
			t.Errorf("%s: body[%d:%d] is not the block text", blk.Ref, blk.Start, blk.End)
		}
	}
	got, err := json.MarshalIndent(body, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	compareGolden(t, "spec-blocks.json", append(got, '\n'))
}

func TestParseSpecBodyFindings(t *testing.T) {
	t.Parallel()

	const spec ItemID = "ACME-SP-0003"
	tests := []struct {
		name       string
		body       string
		wantBlocks []string
		wantCodes  []Code
	}{
		{
			name:       "canonical blocks",
			body:       "## Requirements\n\n### ACME-SP-0003.R1 — One\n\nThe system SHALL do one.\n\n### ACME-SP-0003.R2 — Two\n\nThe system SHALL do two.\n",
			wantBlocks: []string{"ACME-SP-0003.R1", "ACME-SP-0003.R2"},
		},
		{
			name:       "en dash and double hyphen are accepted with a warning",
			body:       "### ACME-SP-0003.R1 – One\n\n### ACME-SP-0003.R2 -- Two\n",
			wantBlocks: []string{"ACME-SP-0003.R1", "ACME-SP-0003.R2"},
			wantCodes:  []Code{CodeWarnReqSeparator, CodeWarnReqSeparator},
		},
		{
			name:      "a padded or lower-case number is a grammar error",
			body:      "### ACME-SP-0003.R02 — Padded\n\n### ACME-SP-0003.r3 — Lower\n",
			wantCodes: []Code{CodeIDGrammar, CodeIDGrammar},
		},
		{
			name:      "a ref of another spec is foreign",
			body:      "### ACME-SP-0004.R1 — Elsewhere\n",
			wantCodes: []Code{CodeReqForeign},
		},
		{
			name:      "a prose heading under Requirements is reported",
			body:      "## Requirements\n\n### Background\n\n## Notes\n\n### Also prose, outside Requirements\n",
			wantCodes: []Code{CodeWarnReqHeading},
		},
		{
			name:      "a ref without a title is prose",
			body:      "## Requirements\n\n### ACME-SP-0003.R1\n",
			wantCodes: []Code{CodeWarnReqHeading},
		},
		{
			name:       "a heading inside a fence is not a boundary",
			body:       "### ACME-SP-0003.R1 — One\n\n~~~\n### ACME-SP-0003.R2 — Fenced\n~~~\n",
			wantBlocks: []string{"ACME-SP-0003.R1"},
		},
		{
			name:       "an indented heading is not a requirement",
			body:       " ### ACME-SP-0003.R1 — Indented\n",
			wantBlocks: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ParseSpecBody(spec, tt.body)
			var refs []string
			for _, b := range got.Blocks {
				refs = append(refs, b.Ref.String())
			}
			if strings.Join(refs, ",") != strings.Join(tt.wantBlocks, ",") {
				t.Errorf("blocks = %v, want %v", refs, tt.wantBlocks)
			}
			var codes []Code
			for _, f := range got.Findings {
				codes = append(codes, f.Code)
			}
			if !equalCodes(codes, tt.wantCodes) {
				t.Errorf("findings = %v, want %v (%+v)", codes, tt.wantCodes, got.Findings)
			}
		})
	}
}

func equalCodes(a, b []Code) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestParseSpecBodyContent(t *testing.T) {
	t.Parallel()

	body := "### ACME-SP-0003.R1 — One\n\nWHEN a thing happens,\nthe system SHALL react.\n\nMore prose.\n\n" +
		"#### Scenario: happy path\n- **GIVEN** a state\n- **WHEN** it happens\n- **THEN** it reacts\n\n" +
		"#### Scenario: empty\n\n##### Deeper heading\n- not a step\n"
	got := ParseSpecBody("ACME-SP-0003", body)
	if len(got.Blocks) != 1 {
		t.Fatalf("blocks = %d", len(got.Blocks))
	}
	blk := got.Blocks[0]
	if blk.Statement != "WHEN a thing happens,\nthe system SHALL react." {
		t.Errorf("statement = %q", blk.Statement)
	}
	if blk.Title != "One" || blk.Line != 1 || blk.Separator != ReqSeparator {
		t.Errorf("heading = %+v", blk)
	}
	if len(blk.Scenarios) != 2 {
		t.Fatalf("scenarios = %+v", blk.Scenarios)
	}
	if s := blk.Scenarios[0]; s.Name != "happy path" || len(s.Steps) != 3 || s.Steps[1] != "**WHEN** it happens" || s.Line != 8 {
		t.Errorf("scenario 0 = %+v", s)
	}
	if s := blk.Scenarios[1]; s.Name != "empty" || len(s.Steps) != 0 {
		t.Errorf("scenario 1 = %+v", s)
	}
	if blk.End != len(body) {
		t.Errorf("a block at the end of the body ends at its end: %d != %d", blk.End, len(body))
	}
}

// revsOf maps each block of a body to its rev.
func revsOf(body string) map[string]Rev {
	out := map[string]Rev{}
	for _, b := range ParseSpecBody("ACME-SP-0003", body).Blocks {
		out[b.Ref.String()] = b.Rev
	}
	return out
}

func TestBlockRevIsolation(t *testing.T) {
	t.Parallel()

	base := "## Requirements\n\n### ACME-SP-0003.R1 — One\n\nThe system SHALL do one.\n\n" +
		"### ACME-SP-0003.R2 — Two\n\nThe system SHALL do two.\n\n#### Scenario: s\n- **WHEN** x\n- **THEN** y\n\n" +
		"### ACME-SP-0003.R3 — Three\n\nThe system SHALL do three.\n"
	before := revsOf(base)
	if len(before) != 3 {
		t.Fatalf("revs = %v", before)
	}

	tests := []struct {
		name      string
		body      string
		changed   []string
		unchanged []string
	}{
		{
			name:      "editing one block changes only its rev",
			body:      strings.Replace(base, "do two.", "do two, twice.", 1),
			changed:   []string{"ACME-SP-0003.R2"},
			unchanged: []string{"ACME-SP-0003.R1", "ACME-SP-0003.R3"},
		},
		{
			name:      "editing a scenario changes the block rev",
			body:      strings.Replace(base, "- **THEN** y", "- **THEN** z", 1),
			changed:   []string{"ACME-SP-0003.R2"},
			unchanged: []string{"ACME-SP-0003.R1", "ACME-SP-0003.R3"},
		},
		{
			name:      "retitling changes the block rev",
			body:      strings.Replace(base, "R1 — One", "R1 — Uno", 1),
			changed:   []string{"ACME-SP-0003.R1"},
			unchanged: []string{"ACME-SP-0003.R2", "ACME-SP-0003.R3"},
		},
		{
			name:      "blank lines between blocks change nothing",
			body:      strings.ReplaceAll(base, "\n\n### ", "\n\n\n\n### "),
			unchanged: []string{"ACME-SP-0003.R1", "ACME-SP-0003.R2", "ACME-SP-0003.R3"},
		},
		{
			name:      "reordering blocks changes nothing",
			body:      "### ACME-SP-0003.R3 — Three\n\nThe system SHALL do three.\n\n" + strings.Replace(base, "### ACME-SP-0003.R3 — Three\n\nThe system SHALL do three.\n", "", 1),
			unchanged: []string{"ACME-SP-0003.R1", "ACME-SP-0003.R2", "ACME-SP-0003.R3"},
		},
		{
			name:      "crlf line endings change nothing",
			body:      strings.ReplaceAll(base, "\n", "\r\n"),
			unchanged: []string{"ACME-SP-0003.R1", "ACME-SP-0003.R2", "ACME-SP-0003.R3"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			after := revsOf(tt.body)
			for _, ref := range tt.changed {
				if after[ref] == before[ref] {
					t.Errorf("%s: rev unchanged %s", ref, after[ref])
				}
			}
			for _, ref := range tt.unchanged {
				if after[ref] != before[ref] {
					t.Errorf("%s: rev %s, want %s", ref, after[ref], before[ref])
				}
			}
		})
	}
}

func TestBlockRevIgnoresFrontMatter(t *testing.T) {
	t.Parallel()

	it := specFixture(t)
	before := revsOf(it.Body)
	it.Title = "Renamed"
	it.Requirements["R1"].Status = "cancelled"
	data, err := SerializeItem(it)
	if err != nil {
		t.Fatalf("SerializeItem(): %v", err)
	}
	again, err := ParseItem(it.Path, data)
	if err != nil {
		t.Fatalf("ParseItem(): %v", err)
	}
	after := revsOf(again.Body)
	for ref, rev := range before {
		if after[ref] != rev {
			t.Errorf("%s: rev %s, want %s", ref, after[ref], rev)
		}
	}
}

func TestRequirementRevVersusBlockRev(t *testing.T) {
	t.Parallel()

	text := "### ACME-SP-0003.R1 — One\n\nThe system SHALL do one.\n"
	entry := &Requirement{Status: "todo"}
	base, err := RequirementRev(text, entry)
	if err != nil {
		t.Fatalf("RequirementRev(): %v", err)
	}
	if !base.Valid() || base == BlockRev(text) {
		t.Fatalf("requirement rev %s must be a rev distinct from the block rev %s", base, BlockRev(text))
	}

	stamped := entry.Clone()
	stamped.Verified = &Verification{Rev: BlockRev(text), Commit: strings.Repeat("a", 40), By: "claude"}
	tests := []struct {
		name  string
		text  string
		entry *Requirement
		same  bool
	}{
		{name: "same inputs", text: text, entry: &Requirement{Status: "todo"}, same: true},
		{name: "trailing blank lines", text: text + "\n\n", entry: entry, same: true},
		{name: "status change", text: text, entry: &Requirement{Status: "done"}},
		{name: "stamp written", text: text, entry: stamped},
		{name: "text change", text: strings.Replace(text, "one", "two", 1), entry: entry},
		{name: "no entry", text: text, entry: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := RequirementRev(tt.text, tt.entry)
			if err != nil {
				t.Fatalf("RequirementRev(): %v", err)
			}
			if (got == base) != tt.same {
				t.Errorf("rev %s vs %s, same = %v", got, base, tt.same)
			}
		})
	}
	if BlockRev(text) != stamped.Verified.Rev {
		t.Error("writing the stamp must not change the block rev it records")
	}
}

func TestRequirementCanonicalJSON(t *testing.T) {
	t.Parallel()

	var nilEntry *Requirement
	if got, _ := nilEntry.CanonicalJSON(); string(got) != "{}" {
		t.Errorf("nil entry = %s", got)
	}
	it := specFixture(t)
	got, err := it.Requirements["R2"].CanonicalJSON()
	if err != nil {
		t.Fatalf("CanonicalJSON(): %v", err)
	}
	want := `{"status":"in_progress","trace":{"code":["internal/core/allocator.go#NextID"],` +
		`"tests":["internal/core/allocator_test.go#TestNextID/stale_counter"]},` +
		`"verified":{"at":"2026-10-01T09:12:00Z","by":"claude","commit":"9c1f0a2e5b7d4c3e8f1a6b2d9e0c7f4a3b5d8e21","rev":"sha256:4e1b9c0d7a3f2e61"},` +
		`"x-owner":"marta"}`
	if string(got) != want {
		t.Errorf("CanonicalJSON() =\n%s\nwant\n%s", got, want)
	}
}

func TestNextRequirementNumber(t *testing.T) {
	t.Parallel()

	spec := func(body string, keys ...string) *Item {
		it := &Item{ID: "ACME-SP-0003", Type: TypeSpec, Body: body}
		if len(keys) > 0 {
			it.Requirements = Requirements{}
			for _, k := range keys {
				it.Requirements[k] = &Requirement{}
			}
		}
		return it
	}
	tests := []struct {
		name    string
		spec    *Item
		inbound []RequirementRef
		want    int
	}{
		{name: "empty spec", spec: spec(""), want: 1},
		{name: "max block plus one", spec: spec("### ACME-SP-0003.R1 — a\n### ACME-SP-0003.R4 — b\n"), want: 5},
		{name: "an orphan map key keeps its number", spec: spec("### ACME-SP-0003.R1 — a\n", "R1", "R7"), want: 8},
		{name: "a misspelled heading keeps its number", spec: spec("### ACME-SP-0003.R09 — a\n"), want: 10},
		{name: "a heading without a title keeps its number", spec: spec("### ACME-SP-0003.R6\n"), want: 7},
		{name: "a malformed key keeps its number", spec: spec("", "R02"), want: 3},
		{
			name:    "an inbound ref keeps a deleted number",
			spec:    spec("### ACME-SP-0003.R1 — a\n"),
			inbound: []RequirementRef{{Spec: "ACME-SP-0003", Number: 12}, {Spec: "ACME-SP-0009", Number: 40}},
			want:    13,
		},
		{name: "a foreign heading is not counted", spec: spec("### ACME-SP-0004.R9 — a\n"), want: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := NextRequirementNumber(tt.spec, tt.inbound); got != tt.want {
				t.Errorf("NextRequirementNumber() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestSpecAllocationScansTheSpecsFolder(t *testing.T) {
	t.Parallel()

	fsys := NewMemFS()
	cfg := &ProjectConfig{Key: "ACME", Schema: 2}
	for _, p := range []string{
		"docs/.pmngr/specs/ACME-SP-0007-a.md",
		"docs/.pmngr/stories/ACME-US-0020-b.md",
	} {
		if err := fsys.MkdirAll(filepath.ToSlash(filepath.Dir(p))); err != nil {
			t.Fatal(err)
		}
		if err := fsys.WriteFile(p, []byte("---\nid: "+string(IDFromFileName(p))+"\n---\n")); err != nil {
			t.Fatal(err)
		}
	}
	alloc := NewAllocator(fsys, "docs", cfg)
	got, err := alloc.Peek(context.Background(), TypeSpec)
	if err != nil {
		t.Fatalf("Peek(): %v", err)
	}
	if got != "ACME-SP-0008" {
		t.Errorf("next spec = %s, want ACME-SP-0008", got)
	}
}

func TestWriteGate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		schema int
		ok     bool
	}{
		{schema: 0}, {schema: 1, ok: true}, {schema: 2, ok: true}, {schema: 3},
	}
	for _, tt := range tests {
		err := (&ProjectConfig{Schema: tt.schema}).WriteGate()
		if (err == nil) != tt.ok {
			t.Errorf("schema %d: WriteGate() = %v", tt.schema, err)
		}
		if err != nil && (!errors.Is(err, ErrReadOnly) || !errors.Is(err, ErrSchemaUnsupported)) {
			t.Errorf("schema %d: %v must be ErrReadOnly and ErrSchemaUnsupported", tt.schema, err)
		}
	}
}

func TestUpgradeProjectSchema(t *testing.T) {
	t.Parallel()

	const rest = "key: ACME\nname: Acme\nworkflow:\n  statuses:\n    - {id: todo, category: todo}\n    - {id: done, category: done}\n"
	tests := []struct {
		name string
		in   string
		want string // empty: no change
	}{
		{name: "one line changes", in: "# the project\nschema: 1 # layout version\n" + rest, want: "# the project\nschema: 2 # layout version\n" + rest},
		{name: "schema in the middle", in: "key: ACME\nschema: 1\nname: Acme\nworkflow:\n  statuses:\n    - {id: done, category: done}\n",
			want: "key: ACME\nschema: 2\nname: Acme\nworkflow:\n  statuses:\n    - {id: done, category: done}\n"},
		{name: "already at 2", in: "schema: 2\n" + rest},
		{name: "never downgrades", in: "schema: 3\n" + rest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := UpgradeProjectSchema([]byte(tt.in), SpecSchema)
			if err != nil {
				t.Fatalf("UpgradeProjectSchema(): %v", err)
			}
			if tt.want == "" {
				if got != nil {
					t.Errorf("want no change, got %q", got)
				}
				return
			}
			if string(got) != tt.want {
				t.Errorf("got\n%s\nwant\n%s", got, tt.want)
			}
		})
	}
}
