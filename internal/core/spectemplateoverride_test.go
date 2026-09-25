package core

import (
	"sort"
	"strings"
	"testing"
)

const (
	tplDir     = "docs/.pmngr/templates"
	tplSpec    = tplDir + "/spec.md"
	tplReq     = tplDir + "/requirement.md"
	tplProject = "schema: 1\nkey: ACME\nname: Acme\nworkflow:\n  statuses:\n    - {id: todo, category: todo}\n    - {id: done, category: done}\n"
)

// customRequirement is a valid, lint-clean requirement override.
const customRequirement = "The <system> SHALL <response>.\n\n#### Scenario: <name>\n" +
	"- **GIVEN** <context>\n- **WHEN** <action>\n- **THEN** <observable result>\n"

// customSpec is a valid, lint-clean spec override with a section of its own.
const customSpec = "## Purpose\n\n<why>\n\n## Non-goals\n\n<what not>\n\n## Requirements\n\n" +
	"### <SPEC-ID>.R1 — <title>\n\n" + customRequirement

func templateCodes(diags []Diagnostic) []string {
	out := make([]string, 0, len(diags))
	for _, d := range diags {
		out = append(out, string(d.Code))
	}
	sort.Strings(out)
	return out
}

// TestLoadSpecTemplates covers R-TPL-1..3: the embedded default, a valid
// override per file, and every way an override falls back.
func TestLoadSpecTemplates(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		files       map[string]string
		wantSpec    string
		wantReq     string
		wantSources [2]string
		wantCodes   []string
	}{
		{
			name:        "no override is the embedded pair",
			wantSpec:    SpecTemplate(),
			wantReq:     RequirementTemplate(),
			wantSources: [2]string{TemplateSourceEmbedded, TemplateSourceEmbedded},
			wantCodes:   []string{},
		},
		{
			name:        "both overrides win",
			files:       map[string]string{tplSpec: customSpec, tplReq: customRequirement},
			wantSpec:    customSpec,
			wantReq:     customRequirement,
			wantSources: [2]string{tplSpec, tplReq},
			wantCodes:   []string{},
		},
		{
			name:        "each file is resolved on its own",
			files:       map[string]string{tplReq: customRequirement},
			wantSpec:    SpecTemplate(),
			wantReq:     customRequirement,
			wantSources: [2]string{TemplateSourceEmbedded, tplReq},
			wantCodes:   []string{},
		},
		{
			name:        "a blank override falls back",
			files:       map[string]string{tplSpec: "  \n\n", tplReq: ""},
			wantSpec:    SpecTemplate(),
			wantReq:     RequirementTemplate(),
			wantSources: [2]string{TemplateSourceEmbedded, TemplateSourceEmbedded},
			wantCodes:   []string{string(CodeWarnTemplateInvalid), string(CodeWarnTemplateInvalid)},
		},
		{
			name:        "a requirement override with a level-3 heading falls back",
			files:       map[string]string{tplReq: "The x SHALL y.\n\n### Not allowed\n"},
			wantSpec:    SpecTemplate(),
			wantReq:     RequirementTemplate(),
			wantSources: [2]string{TemplateSourceEmbedded, TemplateSourceEmbedded},
			wantCodes:   []string{string(CodeWarnTemplateInvalid)},
		},
		{
			name:        "a requirement override leaving a fence open falls back",
			files:       map[string]string{tplReq: "The x SHALL y.\n\n```go\ncode\n"},
			wantSpec:    SpecTemplate(),
			wantReq:     RequirementTemplate(),
			wantSources: [2]string{TemplateSourceEmbedded, TemplateSourceEmbedded},
			wantCodes:   []string{string(CodeWarnTemplateInvalid)},
		},
		{
			name:        "a spec override naming a real spec falls back",
			files:       map[string]string{tplSpec: "## Requirements\n\n### ACME-SP-0003.R1 — Foreign\n\n" + customRequirement},
			wantSpec:    SpecTemplate(),
			wantReq:     RequirementTemplate(),
			wantSources: [2]string{TemplateSourceEmbedded, TemplateSourceEmbedded},
			wantCodes:   []string{string(CodeWarnTemplateInvalid)},
		},
		{
			name:        "an override that is not UTF-8 falls back",
			files:       map[string]string{tplReq: "The x SHALL \xff.\n"},
			wantSpec:    SpecTemplate(),
			wantReq:     RequirementTemplate(),
			wantSources: [2]string{TemplateSourceEmbedded, TemplateSourceEmbedded},
			wantCodes:   []string{string(CodeWarnTemplateInvalid)},
		},
		{
			name:        "lint findings warn but keep the override",
			files:       map[string]string{tplReq: "The system is fast.\n"},
			wantSpec:    SpecTemplate(),
			wantReq:     "The system is fast.\n",
			wantSources: [2]string{TemplateSourceEmbedded, tplReq},
			wantCodes:   []string{string(LintReqScenario), string(LintReqStatement), string(LintReqVague)},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			files := map[string]string{"docs/.pmngr/project.yaml": tplProject}
			for k, v := range tt.files {
				files[k] = v
			}
			got := LoadSpecTemplates(NewMemFSFromMap(files), "docs", nil)
			if got.Spec != tt.wantSpec {
				t.Errorf("Spec = %q, want %q", got.Spec, tt.wantSpec)
			}
			if got.Requirement != tt.wantReq {
				t.Errorf("Requirement = %q, want %q", got.Requirement, tt.wantReq)
			}
			if src := [2]string{got.SpecSource, got.RequirementSource}; src != tt.wantSources {
				t.Errorf("sources = %v, want %v", src, tt.wantSources)
			}
			if codes := templateCodes(got.Diagnostics); strings.Join(codes, ",") != strings.Join(tt.wantCodes, ",") {
				t.Errorf("codes = %v, want %v (%+v)", codes, tt.wantCodes, got.Diagnostics)
			}
			for _, d := range got.Diagnostics {
				if d.Severity != SeverityWarning {
					t.Errorf("%s at %s, want warning", d.Code, d.Severity)
				}
				if d.Path != tplSpec && d.Path != tplReq {
					t.Errorf("%s on %q, want a template path", d.Code, d.Path)
				}
			}
		})
	}
}

// TestTemplateLintCapsAtWarning: a rule the project raises to error is still a
// warning on a template, and a rule at off reports nothing (R-TPL-3).
func TestTemplateLintCapsAtWarning(t *testing.T) {
	t.Parallel()
	cfg, err := LoadProjectConfig([]byte(tplProject +
		"specs:\n  lint:\n    severity: error\n    rules:\n      LINT-REQ-VAGUE: off\n"))
	if err != nil {
		t.Fatal(err)
	}
	fs := NewMemFSFromMap(map[string]string{tplReq: "The system is fast.\n"})
	got := LoadSpecTemplates(fs, "docs/.pmngr", cfg)
	if got.RequirementSource != tplReq {
		t.Fatalf("source = %q, want the override", got.RequirementSource)
	}
	if codes := templateCodes(got.Diagnostics); strings.Join(codes, ",") != "LINT-REQ-SCENARIO,LINT-REQ-STATEMENT" {
		t.Errorf("codes = %v", codes)
	}
	for _, d := range got.Diagnostics {
		if d.Severity != SeverityWarning || d.Line != 1 || strings.Contains(d.Message, string(templateProbeID)) {
			t.Errorf("diagnostic %+v", d)
		}
	}
}

// TestExportSpecTemplates covers creation, idempotence, the edited-file guard,
// --force and --dry-run.
func TestExportSpecTemplates(t *testing.T) {
	t.Parallel()
	actions := func(got []TemplateExport) string {
		var b []string
		for _, e := range got {
			b = append(b, e.Name+"="+e.Action)
		}
		return strings.Join(b, ",")
	}
	fs := NewMemFSFromMap(map[string]string{"docs/.pmngr/project.yaml": tplProject})

	got, err := ExportSpecTemplates(fs, "docs", false, true)
	if err != nil || actions(got) != "spec.md=created,requirement.md=created" {
		t.Fatalf("dry run = %v, %v", actions(got), err)
	}
	if _, err := fs.Stat(tplDir); err == nil {
		t.Fatal("a dry run wrote the templates folder")
	}

	got, err = ExportSpecTemplates(fs, "docs", false, false)
	if err != nil || actions(got) != "spec.md=created,requirement.md=created" {
		t.Fatalf("first export = %v, %v", actions(got), err)
	}
	if data, _ := fs.ReadFile(tplSpec); string(data) != SpecTemplate() {
		t.Errorf("spec.md = %q", data)
	}
	if loaded := LoadSpecTemplates(fs, "docs", nil); len(loaded.Diagnostics) != 0 || loaded.SpecSource != tplSpec {
		t.Errorf("the exported templates do not load clean: %+v", loaded)
	}

	got, err = ExportSpecTemplates(fs, "docs", false, false)
	if err != nil || actions(got) != "spec.md=unchanged,requirement.md=unchanged" {
		t.Fatalf("second export = %v, %v", actions(got), err)
	}

	if err := fs.WriteFile(tplReq, []byte(customRequirement)); err != nil {
		t.Fatal(err)
	}
	got, err = ExportSpecTemplates(fs, "docs", false, false)
	if err != nil || actions(got) != "spec.md=unchanged,requirement.md=skipped" {
		t.Fatalf("export over an edit = %v, %v", actions(got), err)
	}
	if data, _ := fs.ReadFile(tplReq); string(data) != customRequirement {
		t.Error("an edited template was overwritten without force")
	}

	got, err = ExportSpecTemplates(fs, "docs", true, true)
	if err != nil || actions(got) != "spec.md=unchanged,requirement.md=overwritten" {
		t.Fatalf("forced dry run = %v, %v", actions(got), err)
	}
	if data, _ := fs.ReadFile(tplReq); string(data) != customRequirement {
		t.Error("a forced dry run wrote")
	}
	if _, err = ExportSpecTemplates(fs, "docs", true, false); err != nil {
		t.Fatal(err)
	}
	if data, _ := fs.ReadFile(tplReq); string(data) != RequirementTemplate() {
		t.Error("force did not restore the embedded template")
	}
}

// TestIndexTemplates: templates/ is part of the layout (R-LOC-7). Its files
// are never items, an invalid one carries W-TEMPLATE-INVALID, and anything
// else inside it is W-LAYOUT-STRAY — on a full build and on a file event.
func TestIndexTemplates(t *testing.T) {
	t.Parallel()
	fs := NewMemFSFromMap(map[string]string{
		"docs/.pmngr/project.yaml": tplProject,
		tplSpec:                    customSpec,
		tplReq:                     "",
		tplDir + "/story.md":       "## Description\n",
	})
	projects, err := DiscoverProjects(fs, ".")
	if err != nil {
		t.Fatal(err)
	}
	ix := NewIndex(fs, projects)
	if _, err := ix.Build(t.Context(), true); err != nil {
		t.Fatal(err)
	}
	findings := func() map[string]string {
		out := map[string]string{}
		for _, w := range ix.Warnings() {
			out[w.Path] += string(w.Code) + " "
		}
		return out
	}
	got := findings()
	want := map[string]string{
		tplReq:               string(CodeWarnTemplateInvalid) + " ",
		tplDir + "/story.md": string(idxCodeLayoutStray) + " ",
	}
	if len(got) != len(want) {
		t.Errorf("findings = %v, want %v", got, want)
	}
	for p, codes := range want {
		if got[p] != codes {
			t.Errorf("%s: %q, want %q", p, got[p], codes)
		}
	}
	if n := ix.Stats().Items; n != 0 {
		t.Errorf("the index holds %d items, want none", n)
	}

	if err := fs.WriteFile(tplReq, []byte(customRequirement)); err != nil {
		t.Fatal(err)
	}
	if _, err := ix.ApplyFileEvents(t.Context(), []FileEvent{{Kind: FileModified, Path: tplReq}}); err != nil {
		t.Fatal(err)
	}
	if codes, ok := findings()[tplReq]; ok {
		t.Errorf("a fixed template still reports %s", codes)
	}
}
