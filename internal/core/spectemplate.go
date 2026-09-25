package core

import (
	_ "embed" // the body templates ship inside the binary and core.wasm
	"strings"
)

// This file ships the two spec body templates of docs/03 section 21.1
// (GIT-US-0111): a whole spec — purpose, scope, glossary and one example
// requirement block — and the text of one requirement block below its heading.
//
// They live in templates/ as plain Markdown so that there is one source: the Go
// core embeds them, and the web editor imports the very same files at build
// time (web/src/features/editor/templates.ts). Both are linted clean by the
// default specs.lint rules (spectemplate_test.go).

// specTemplate is the body a new spec starts from.
//
//go:embed templates/spec.md
var specTemplate string

// requirementTemplate is the block text of a new requirement: an EARS statement
// and one scenario, everything below the `### <REF> — <title>` heading.
//
//go:embed templates/requirement.md
var requirementTemplate string

// SpecIDPlaceholder stands for the spec's own id in the requirement heading of
// the spec template. A new spec's id is allocated by Create, so a template
// cannot name it; Create replaces the placeholder in requirement headings of
// a new spec body with the id it allocated (ExpandSpecIDPlaceholder).
const SpecIDPlaceholder = "<SPEC-ID>"

// SpecTemplate returns the body template of a new spec, with SpecIDPlaceholder
// where the spec id goes.
func SpecTemplate() string { return specTemplate }

// RequirementTemplate returns the block text template of a new requirement.
func RequirementTemplate() string { return requirementTemplate }

// ExpandSpecIDPlaceholder replaces SpecIDPlaceholder with id in every level-3
// heading of body that opens with `<SPEC-ID>.R` — the requirement headings of
// the spec template. Nothing else in the body is touched, so prose that
// mentions the placeholder keeps it.
func ExpandSpecIDPlaceholder(body string, id ItemID) string {
	prefix := "### " + SpecIDPlaceholder + ".R"
	if id == "" || !strings.Contains(body, prefix) {
		return body
	}
	lines := strings.SplitAfter(body, "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, prefix) {
			lines[i] = "### " + string(id) + ".R" + line[len(prefix):]
		}
	}
	return strings.Join(lines, "")
}
