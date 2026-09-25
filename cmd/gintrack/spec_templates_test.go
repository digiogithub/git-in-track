package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/digiogithub/git-in-track/internal/core"
)

// TestSpecTemplatesExport covers `gintrack spec templates export`: a dry run
// writes nothing, the export is idempotent, an edited file survives without
// --force, and doctor lints the override files (GIT-US-0162).
func TestSpecTemplatesExport(t *testing.T) {
	h := newHarness(t)
	root := specRepo(t)
	h.mustRun("add", root)
	dir := filepath.Join(root, "docs", ".pmngr", "templates")

	export := func(args ...string) map[string]string {
		t.Helper()
		out := h.mustRun(append([]string{"spec", "templates", "export", "--json"}, args...)...)
		var payload specTemplatesExportPayload
		if err := json.Unmarshal([]byte(out), &payload); err != nil {
			t.Fatalf("decode %q: %v", out, err)
		}
		got := map[string]string{}
		for _, e := range payload.Templates {
			got[e.Path] = e.Action
		}
		return got
	}
	want := func(spec, req string) map[string]string {
		return map[string]string{
			"docs/.pmngr/templates/spec.md":        spec,
			"docs/.pmngr/templates/requirement.md": req,
		}
	}
	same := func(a, b map[string]string) bool {
		if len(a) != len(b) {
			return false
		}
		for k, v := range a {
			if b[k] != v {
				return false
			}
		}
		return true
	}

	if got := export("--dry-run"); !same(got, want(core.TemplateCreated, core.TemplateCreated)) {
		t.Errorf("dry run = %v", got)
	}
	if _, err := os.Stat(dir); err == nil {
		t.Fatal("a dry run created the templates folder")
	}
	if got := export(); !same(got, want(core.TemplateCreated, core.TemplateCreated)) {
		t.Errorf("export = %v", got)
	}
	if got := export(); !same(got, want(core.TemplateUnchanged, core.TemplateUnchanged)) {
		t.Errorf("second export = %v", got)
	}

	t.Run("doctor accepts the exported templates", func(t *testing.T) {
		out, _, _ := h.run("doctor")
		if strings.Contains(out, "templates/") {
			t.Errorf("doctor reports the templates:\n%s", out)
		}
	})

	if err := os.WriteFile(filepath.Join(dir, "requirement.md"), []byte("### Not a block\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := export(); !same(got, want(core.TemplateUnchanged, core.TemplateSkipped)) {
		t.Errorf("export over an edit = %v", got)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "requirement.md")); string(data) != "### Not a block\n" {
		t.Error("an edited template was overwritten without --force")
	}

	t.Run("doctor reports an invalid override", func(t *testing.T) {
		out, _, _ := h.run("doctor")
		if !strings.Contains(out, "W-TEMPLATE-INVALID") || !strings.Contains(out, "requirement.md") {
			t.Errorf("doctor does not report the invalid template:\n%s", out)
		}
	})

	if got := export("--force"); !same(got, want(core.TemplateUnchanged, core.TemplateOverwritten)) {
		t.Errorf("forced export = %v", got)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "requirement.md")); string(data) != core.RequirementTemplate() {
		t.Error("--force did not restore the embedded template")
	}

	t.Run("an unknown project is not found", func(t *testing.T) {
		if _, stderr, code := h.run("spec", "templates", "export", "--project", "NOPE"); code != exitNotFound {
			t.Errorf("exit %d, want %d\n%s", code, exitNotFound, stderr)
		}
	})
}

// TestItemNewSpecUsesTheTemplate: `item new --type spec` without --body
// starts from the spec template in effect — the embedded one, then the
// project's override — and an explicit body wins (GIT-US-0162).
func TestItemNewSpecUsesTheTemplate(t *testing.T) {
	h := newHarness(t)
	root := specRepo(t)
	h.mustRun("add", root)

	read := func(out string) string {
		t.Helper()
		var payload struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal([]byte(out), &payload); err != nil || payload.Path == "" {
			t.Fatalf("decode %q: %v", out, err)
		}
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(payload.Path)))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}

	embedded := read(h.mustRun("item", "new", "--type", "spec", "--title", "Embedded", "--json"))
	if !strings.Contains(embedded, "## Glossary\n") || strings.Contains(embedded, core.SpecIDPlaceholder) {
		t.Errorf("no body did not start from the embedded template:\n%s", embedded)
	}

	dir := filepath.Join(root, "docs", ".pmngr", "templates")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	const custom = "## Purpose\n\nOur house style.\n"
	if err := os.WriteFile(filepath.Join(dir, "spec.md"), []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := read(h.mustRun("item", "new", "--type", "spec", "--title", "Ours", "--json")); !strings.Contains(got, "Our house style.") {
		t.Errorf("no body did not start from the override:\n%s", got)
	}
	explicit := read(h.mustRun("item", "new", "--type", "spec", "--title", "Mine", "--body", "## Purpose\n\nMine.\n", "--json"))
	if strings.Contains(explicit, "Our house style.") || !strings.Contains(explicit, "Mine.") {
		t.Errorf("an explicit body was replaced:\n%s", explicit)
	}
}
