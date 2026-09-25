package core

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// This file resolves the spec template overrides of ADR-038 (docs/03 R-LOC-7,
// R-TPL-1..5, GIT-US-0162): a backlog may replace either embedded template
// with a file of the same name under .pmngr/templates/. Everything is read
// through the FS the caller supplies, so the CLI, the companion and the WASM
// core in the browser resolve the same text.

// TemplatesDirName is the folder of the template overrides inside a backlog.
const TemplatesDirName = "templates"

// The file names of the two templates, the same as internal/core/templates/.
const (
	SpecTemplateFileName        = "spec.md"
	RequirementTemplateFileName = "requirement.md"
)

// CodeWarnTemplateInvalid reports an override that cannot be used; the
// embedded template stands in for it (R-TPL-2).
const CodeWarnTemplateInvalid Code = "W-TEMPLATE-INVALID"

// TemplateSourceEmbedded is the Source of a template that comes from the
// binary rather than from a file.
const TemplateSourceEmbedded = "embedded"

// templateProbeID stands for the spec a template is parsed and linted as. Its
// key cannot be a real project's in practice, so a heading naming any real
// spec is foreign to it (E-REQ-FOREIGN).
const templateProbeID ItemID = "TEMPLATE-SP-0000"

// requirementProbePrefix wraps the requirement template into one block so it
// can be linted; requirementProbeLines is how many lines it adds above the
// template's first line.
const (
	requirementProbePrefix = "## Requirements\n\n### " + string(templateProbeID) + ".R1 — Template\n\n"
	requirementProbeLines  = 4
)

// EmbeddedTemplate is one template as the binary ships it.
type EmbeddedTemplate struct {
	Name string `json:"name"`
	Text string `json:"text"`
}

// EmbeddedSpecTemplates returns the shipped templates under the file names an
// override uses, in a stable order: spec first, then requirement.
func EmbeddedSpecTemplates() []EmbeddedTemplate {
	return []EmbeddedTemplate{
		{Name: SpecTemplateFileName, Text: specTemplate},
		{Name: RequirementTemplateFileName, Text: requirementTemplate},
	}
}

// SpecTemplates is the effective pair of templates of one backlog.
type SpecTemplates struct {
	// Spec is the body a new spec starts from, with SpecIDPlaceholder where
	// the spec id goes.
	Spec string `json:"spec"`
	// Requirement is the block text a new requirement starts from.
	Requirement string `json:"requirement"`
	// SpecSource and RequirementSource say where each came from: "embedded",
	// or the vault-relative path of the override file.
	SpecSource        string `json:"specSource"`
	RequirementSource string `json:"requirementSource"`
	// Diagnostics are the findings about the override files: W-TEMPLATE-INVALID
	// for one that fell back, and LINT-REQ-* at warning for a valid one.
	Diagnostics []Diagnostic `json:"diagnostics"`
}

// TemplatesDir returns the override folder of a backlog. projectDir may be the
// .pmngr folder or the documentation folder that contains it.
func TemplatesDir(projectDir string) string {
	return joinPath(BacklogDir(projectDir), TemplatesDirName)
}

// LoadSpecTemplates resolves the effective templates of one backlog: each
// valid override file wins over its embedded copy, each missing or invalid one
// yields the embedded copy (R-TPL-1, R-TPL-2). cfg supplies specs.lint for the
// lint of a valid override (R-TPL-3); nil reads as the defaults. An error
// reading a file is not returned: it is the W-TEMPLATE-INVALID of that file.
func LoadSpecTemplates(fs FS, projectDir string, cfg *ProjectConfig) SpecTemplates {
	out := SpecTemplates{
		Spec: specTemplate, Requirement: requirementTemplate,
		SpecSource: TemplateSourceEmbedded, RequirementSource: TemplateSourceEmbedded,
		Diagnostics: []Diagnostic{},
	}
	dir := TemplatesDir(projectDir)
	specPath := joinPath(dir, SpecTemplateFileName)
	text, diags, ok := CheckTemplateFile(fs, specPath, cfg)
	out.Diagnostics = append(out.Diagnostics, diags...)
	if ok {
		out.Spec, out.SpecSource = text, specPath
	}
	reqPath := joinPath(dir, RequirementTemplateFileName)
	text, diags, ok = CheckTemplateFile(fs, reqPath, cfg)
	out.Diagnostics = append(out.Diagnostics, diags...)
	if ok {
		out.Requirement, out.RequirementSource = text, reqPath
	}
	return out
}

// CheckTemplateFile reads one override file and reports whether it is usable.
// A missing file is not usable and has no findings. A file whose base name is
// neither template name is not a template: it is not usable and has no
// findings either (the index reports it W-LAYOUT-STRAY).
func CheckTemplateFile(fs FS, filePath string, cfg *ProjectConfig) (string, []Diagnostic, bool) {
	name := filePath[strings.LastIndex(filePath, "/")+1:]
	if name != SpecTemplateFileName && name != RequirementTemplateFileName {
		return "", nil, false
	}
	data, err := fs.ReadFile(filePath)
	if err != nil {
		if errors.Is(err, ErrNotExist) {
			return "", nil, false
		}
		return "", []Diagnostic{templateInvalid(filePath, 0, "cannot be read: "+err.Error())}, false
	}
	text := string(data)
	if name == SpecTemplateFileName {
		return checkSpecTemplate(filePath, text, cfg)
	}
	return checkRequirementTemplate(filePath, text, cfg)
}

// checkSpecTemplate validates and lints a spec template override.
func checkSpecTemplate(filePath, text string, cfg *ProjectConfig) (string, []Diagnostic, bool) {
	if d, bad := templateEncoding(filePath, text); bad {
		return "", []Diagnostic{d}, false
	}
	body := ExpandSpecIDPlaceholder(text, templateProbeID)
	var invalid []Diagnostic
	for _, f := range ParseSpecBody(templateProbeID, body).Findings {
		if f.Severity == SeverityError {
			invalid = append(invalid, templateInvalid(filePath, f.Line,
				fmt.Sprintf("%s: %s", f.Code, probeFree(f.Message))))
		}
	}
	if len(invalid) > 0 {
		return "", invalid, false
	}
	var lint []Diagnostic
	for _, f := range LintSpec(templateProbeID, body, cfg.SpecLint()) {
		lint = append(lint, templateLint(filePath, f, f.Line))
	}
	return text, lint, true
}

// checkRequirementTemplate validates and lints a requirement template override:
// the text below one heading, which must not end the block it is written into.
func checkRequirementTemplate(filePath, text string, cfg *ProjectConfig) (string, []Diagnostic, bool) {
	if d, bad := templateEncoding(filePath, text); bad {
		return "", []Diagnostic{d}, false
	}
	var fence fenceTracker
	for i, line := range strings.Split(text, "\n") {
		if fence.step(line) {
			continue
		}
		if lvl := atxLevel(line); lvl >= 1 && lvl <= 3 {
			return "", []Diagnostic{templateInvalid(filePath, i+1, fmt.Sprintf(
				"line %d is a level-%d heading, which would end the requirement block; use #### or deeper", i+1, lvl))}, false
		}
	}
	if fence.inside() {
		return "", []Diagnostic{templateInvalid(filePath, 0, "the text leaves a fenced code block open")}, false
	}
	var lint []Diagnostic
	for _, f := range LintSpec(templateProbeID, requirementProbePrefix+text, cfg.SpecLint()) {
		line := f.Line - requirementProbeLines
		if line < 1 {
			line = 1
		}
		lint = append(lint, templateLint(filePath, f, line))
	}
	return text, lint, true
}

// templateEncoding refuses a file that is not UTF-8 or holds nothing but
// white space.
func templateEncoding(filePath, text string) (Diagnostic, bool) {
	switch {
	case !utf8.ValidString(text):
		return templateInvalid(filePath, 0, "is not valid UTF-8"), true
	case strings.TrimSpace(text) == "":
		return templateInvalid(filePath, 0, "is empty"), true
	}
	return Diagnostic{}, false
}

// templateInvalid is the W-TEMPLATE-INVALID finding of one override.
func templateInvalid(filePath string, line int, reason string) Diagnostic {
	return Diagnostic{
		Code: CodeWarnTemplateInvalid, Severity: SeverityWarning, Path: filePath, Line: line,
		Message: "template override " + reason + "; the embedded template is used instead",
	}
}

// templateLint reports one lint finding on a template at warning at most: a
// template is never rejected by lint (R-TPL-3).
func templateLint(filePath string, f LintFinding, line int) Diagnostic {
	return Diagnostic{
		Code: f.Rule, Severity: SeverityWarning, Path: filePath, Line: line,
		Message: "template: " + probeFree(f.Message),
	}
}

// probeFree writes the probe id back as the placeholder the author wrote.
func probeFree(msg string) string {
	return strings.ReplaceAll(msg, string(templateProbeID), SpecIDPlaceholder)
}

// The outcomes of exporting one template (ADR-038 decision 9).
const (
	TemplateCreated     = "created"
	TemplateUnchanged   = "unchanged"
	TemplateSkipped     = "skipped"
	TemplateOverwritten = "overwritten"
)

// TemplateExport is what ExportSpecTemplates did, or would do, with one file.
type TemplateExport struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	Action string `json:"action"`
}

// ExportSpecTemplates writes both embedded templates into the override folder
// of a backlog. A missing file is created; a byte-identical one is left alone,
// so exporting twice writes nothing the second time; a file that differs was
// edited by the team and is skipped unless force is set. dryRun reports the
// same outcomes and writes nothing.
func ExportSpecTemplates(fs FS, projectDir string, force, dryRun bool) ([]TemplateExport, error) {
	dir := TemplatesDir(projectDir)
	out := make([]TemplateExport, 0, 2)
	for _, tpl := range EmbeddedSpecTemplates() {
		target := joinPath(dir, tpl.Name)
		entry := TemplateExport{Name: tpl.Name, Path: target, Action: TemplateCreated}
		current, err := fs.ReadFile(target)
		switch {
		case err == nil && string(current) == tpl.Text:
			entry.Action = TemplateUnchanged
		case err == nil && !force:
			entry.Action = TemplateSkipped
		case err == nil:
			entry.Action = TemplateOverwritten
		case !errors.Is(err, ErrNotExist):
			return out, fmt.Errorf("export template %s: %w", target, err)
		}
		if !dryRun && (entry.Action == TemplateCreated || entry.Action == TemplateOverwritten) {
			if err := fs.MkdirAll(dir); err != nil {
				return out, fmt.Errorf("export template %s: %w", target, err)
			}
			if err := fs.WriteFile(target, []byte(tpl.Text)); err != nil {
				return out, fmt.Errorf("export template %s: %w", target, err)
			}
		}
		out = append(out, entry)
	}
	return out, nil
}
