// Package agentcfg renders the Pando-side configuration of the agent panel
// (ADR-035): the `.pando.toml` with its `[AGUI]` allow-list, the persona and
// the routing skill.
//
// Two callers share it so that the file shape cannot drift: `gintrack agent
// init` writes it into a repository, and the managed AG-UI instance of
// `gintrack serve` (ADR-039, GIT-US-0185) writes it into the instance's own
// cache directory. It is pure text rendering: no filesystem, no os/exec.
package agentcfg

import (
	"embed"
	"fmt"
	"strings"
	"text/template"
)

//go:embed templates/*.tmpl
var templates embed.FS

// The generated file names, relative to the directory the configuration lives in.
const (
	PandoConfigName = ".pando.toml"
	PersonaName     = "agents/personas/backlog-assistant.md"
	SkillName       = "agents/skills/gintrack-search/SKILL.md"
)

// The template file names Render accepts.
const (
	TemplatePando   = "pando.toml.tmpl"
	TemplatePersona = "persona.md.tmpl"
	TemplateSkill   = "skill.md.tmpl"
)

// PersonaID is the persona and profile name the generated configuration points
// at. It is also the route the browser posts a run to:
// POST {AGUI.Path}/backlog-assistant.
const PersonaID = "backlog-assistant"

// DefaultAGUIPath is Pando's own default AG-UI route prefix, stated explicitly
// in the generated file so the companion and Pando cannot drift apart.
const DefaultAGUIPath = "/api/v1/agui"

// DefaultAGUIPort is the loopback port `gintrack agent init` gives the AG-UI
// listener.
const DefaultAGUIPort = 8090

// DefaultMaxRuns is Pando's own backstop on concurrent AG-UI runs.
const DefaultMaxRuns = 4

// Tools is the glob allow-list written into `[AGUI] Tools`. Pando matches a glob
// (path.Match semantics) against each tool's Info().Name and keeps only the
// tools that match, so this is what turns the coder agent into something closer
// to a backlog assistant. MCP tools are named `<server>_<tool>`, which is why
// the gintrack entry is `gintrack_*`.
//
// Only the KB tools that READ are listed. `kb_add_document`,
// `kb_delete_document` and the memory `remember`/`forget` path mirror a document
// back to disk through a serializer that emits Pando's typed keys alone, so a
// call against a file of this repository would rewrite it without `id`,
// `status` or `parent` (GIT-EP-0020).
var Tools = []string{
	"gintrack_*",
	"kb_search_documents",
	"kb_get_document",
	"kb_related_documents",
	"code_hybrid_search",
	"code_find_symbol",
}

// WritingKBTools are the Pando tools that mirror a document to disk. They are
// named here so a test can assert Tools admits none of them.
var WritingKBTools = []string{
	"kb_add_document",
	"kb_delete_document",
	"remember",
	"forget",
}

// CodeTools are the entries of Tools that need a Pando code project. A managed
// AG-UI instance has none (its own database indexes the documentation folder
// only), so it drops them rather than offer tools that always answer nothing.
var CodeTools = []string{"code_hybrid_search", "code_find_symbol"}

// WithoutCodeTools returns tools minus CodeTools.
func WithoutCodeTools(tools []string) []string {
	out := make([]string, 0, len(tools))
	for _, t := range tools {
		skip := false
		for _, c := range CodeTools {
			if t == c {
				skip = true
			}
		}
		if !skip {
			out = append(out, t)
		}
	}
	return out
}

// TemplateData is what the embedded templates are rendered against.
type TemplateData struct {
	RepoID            string
	RepoPath          string
	AGUIPath          string
	AGUIHost          string
	AGUIPort          int
	MaxConcurrentRuns int
	Persona           string
	Tools             []string
	MCPURL            string
	Token             bool
	Encrypted         bool
	AuthToken         string
	BearerHeader      string
	KBPath            string
}

// Render renders one embedded template.
func Render(name string, data TemplateData) ([]byte, error) {
	tmpl, err := template.New(name).Funcs(template.FuncMap{"toml": TOMLString}).
		ParseFS(templates, "templates/"+name)
	if err != nil {
		return nil, fmt.Errorf("parse the %s template: %w", name, err)
	}
	var buf strings.Builder
	if execErr := tmpl.Execute(&buf, data); execErr != nil {
		return nil, fmt.Errorf("render the %s template: %w", name, execErr)
	}
	return []byte(buf.String()), nil
}

// TOMLString renders a value as a TOML string. It prefers a literal string,
// which needs no escaping at all, and falls back to a basic string when the
// value contains a quote, a backslash or a control character.
func TOMLString(v any) string {
	s := fmt.Sprint(v)
	if !strings.ContainsAny(s, "'\n\r\t\\") {
		return "'" + s + "'"
	}
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
