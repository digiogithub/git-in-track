package core

import (
	"context"
	"strings"
	"testing"
)

// TestSpecTemplatesLintClean holds the shipped templates to the grammar they
// teach: every lint rule at error, the default vague words, no finding.
func TestSpecTemplatesLintClean(t *testing.T) {
	t.Parallel()
	strict := &SpecLintConfig{Severity: LintError}
	const id ItemID = "ACME-SP-0007"

	t.Run("spec template", func(t *testing.T) {
		t.Parallel()
		body := ExpandSpecIDPlaceholder(SpecTemplate(), id)
		parsed := ParseSpecBody(id, body)
		if len(parsed.Findings) != 0 {
			t.Errorf("parse findings: %+v", parsed.Findings)
		}
		if len(parsed.Blocks) != 1 || parsed.Blocks[0].Ref.String() != "ACME-SP-0007.R1" {
			t.Fatalf("blocks = %+v, want the one example block ACME-SP-0007.R1", parsed.Blocks)
		}
		if got := LintSpec(id, body, strict); len(got) != 0 {
			t.Errorf("LintSpec() = %+v, want none", got)
		}
		for _, section := range []string{"## Purpose\n", "## Scope\n", "## Glossary\n", "## Requirements\n"} {
			if !strings.Contains(body, section) {
				t.Errorf("spec template lacks %q", strings.TrimSpace(section))
			}
		}
	})

	t.Run("requirement template", func(t *testing.T) {
		t.Parallel()
		body := "## Requirements\n\n### " + string(id) + ".R3 — Example\n\n" + RequirementTemplate()
		if got := LintSpec(id, body, strict); len(got) != 0 {
			t.Errorf("LintSpec() = %+v, want none", got)
		}
	})

	t.Run("the spec example block is the requirement template", func(t *testing.T) {
		t.Parallel()
		if !strings.HasSuffix(SpecTemplate(), "\n\n"+RequirementTemplate()) {
			t.Errorf("the example block of the spec template drifted from the requirement template")
		}
	})
}

func TestExpandSpecIDPlaceholder(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, body, want string
		id               ItemID
	}{
		{"heading", "### <SPEC-ID>.R1 — X\n", "### ACME-SP-0001.R1 — X\n", "ACME-SP-0001"},
		{"prose is kept", "Write <SPEC-ID>.R1 here.\n", "Write <SPEC-ID>.R1 here.\n", "ACME-SP-0001"},
		{"no id", "### <SPEC-ID>.R1 — X\n", "### <SPEC-ID>.R1 — X\n", ""},
		{"no trailing newline", "a\n### <SPEC-ID>.R2 — Y", "a\n### ACME-SP-0002.R2 — Y", "ACME-SP-0002"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := ExpandSpecIDPlaceholder(tt.body, tt.id); got != tt.want {
				t.Errorf("ExpandSpecIDPlaceholder() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestCreateSpecFromTemplate creates a spec from the shipped template: the
// example heading names the allocated id and the file validates clean.
func TestCreateSpecFromTemplate(t *testing.T) {
	t.Parallel()
	store, _ := specStore(t, "2")
	it, err := store.Create(context.Background(), ItemDraft{Type: TypeSpec, Title: "Sign-in", Body: SpecTemplate()})
	if err != nil {
		t.Fatalf("Create(): %v", err)
	}
	if !strings.Contains(it.Body, "### "+string(it.ID)+".R1 — ") || strings.Contains(it.Body, SpecIDPlaceholder) {
		t.Errorf("body = %q, want the placeholder replaced by %s", it.Body, it.ID)
	}
	for _, d := range ValidateItem(it, store.cfg) {
		if d.Severity == SeverityError {
			t.Errorf("diagnostic %+v", d)
		}
	}

	t.Run("a story body keeps the placeholder", func(t *testing.T) {
		t.Parallel()
		store, _ := specStore(t, "2")
		story, err := store.Create(context.Background(), ItemDraft{Type: TypeStory, Title: "S", Body: "### <SPEC-ID>.R1 — x\n"})
		if err != nil {
			t.Fatalf("Create(): %v", err)
		}
		if !strings.Contains(story.Body, SpecIDPlaceholder) {
			t.Errorf("story body = %q", story.Body)
		}
	})
}
