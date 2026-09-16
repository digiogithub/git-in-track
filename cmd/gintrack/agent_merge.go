package main

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"
)

// This file implements the merge half of `gintrack agent init`: a repository
// that already has a `.pando.toml` keeps it, and the generated configuration is
// folded into it instead of replacing it (GIT-T-0227).
//
// The merge is deliberately a line-level text edit and never a decode/re-encode
// round trip. No TOML library available here preserves comments — go-toml/v2
// and BurntSushi/toml have no comment API at all, and go-toml v1's
// SetWithComment is never populated by its parser — so a round trip would erase
// the rationale the template's comments carry (three of them are asserted by
// the tests), rewrite the 'literal' strings tomlString emits as "basic" ones and
// collapse the multi-line arrays. So: locate a table by its header line,
// rewrite only the lines of the keys gintrack owns, and insert whole missing
// blocks verbatim from the rendered template, comments included.

// agentOwnedKeys are the keys `gintrack agent init` writes, per table, in the
// tables it shares with the user. Everything else in those tables is the
// user's: a Pando configuration is a Pando-wide file, not a gintrack artifact.
var agentOwnedKeys = map[string][]string{
	"AGUI": {
		"Enabled", "Path", "Host", "Port", "Agents", "AllowedOrigins",
		"RequireToken", "FrontendTools", "HumanInTheLoop", "AutoApprove",
		"MaxConcurrentRuns", "Persona", "Tools", "Mesnada",
	},
	"ToolDiscovery":     {"Enabled", "Mode"},
	"MCPGateway":        {"Enabled"},
	"PersonaAutoSelect": {"Enabled", "PersonaPath"},
	"Skills":            {"Enabled", "Paths"},
	"Remembrances":      {"Enabled", "KBPath", "KBAutoImport", "KBWatch"},
	"MCPServer":         {"HttpEnabled", "StdioEnabled"},
}

// agentDivergenceReasons is the one-line explanation printed next to a key the
// merge refused to overwrite. A table's entry is the fallback for its keys.
var agentDivergenceReasons = map[string]string{
	"AGUI":                   "the AG-UI listener the browser reaches through the companion is configured from this value",
	"AGUI.Tools":             "without this allow-list the AG-UI agent is Pando's full coder agent, bash/edit/write included",
	"AGUI.AllowedOrigins":    "the companion proxy strips the browser Origin header, so an entry here widens the surface without adding a check",
	"ToolDiscovery":          "with the MCP gateway on, gintrack tools are reached through tool_search instead of `gintrack_*`, which the [AGUI] Tools allow-list and the per-tool approval cannot see",
	"MCPGateway":             "with the MCP gateway on, gintrack tools are reached through tool_search instead of `gintrack_*`, which the [AGUI] Tools allow-list and the per-tool approval cannot see",
	"PersonaAutoSelect":      "Pando loads the generated persona from this directory",
	"Skills":                 "Pando loads the generated skill from this directory",
	"Remembrances":           "this repository's own documentation folder is indexed from this setting",
	"Remembrances.KBPath":    "Pando's KB walk applies no exclusions at all, so this must be the repository's documentation folder: a repository root would be indexed whole, and a directory outside the repository indexes a copy nobody edits",
	"Remembrances.KBWatch":   "the watcher performs no file write of any kind, so it is safe to leave on; with it off an edit to an item is not searchable until the next full import",
	"MCPServer":              "the generated configuration expects Pando's own MCP server in this state",
	"MCPServer.HttpEnabled":  "Pando's own MCP transport auto-approves every tool call globally, so any client reaching the port drives a code-executing agent",
	"MCPServer.StdioEnabled": "the generated configuration expects Pando's own stdio MCP server in this state",
}

// agentDivergence is one key the merge left alone because the file already had
// it with another value.
type agentDivergence struct {
	Table       string `json:"table"`
	Key         string `json:"key"`
	Have        string `json:"have"`
	Recommended string `json:"recommended"`
	Reason      string `json:"reason"`
}

// agentOwnedTables are the tables gintrack owns outright: they are created by
// `agent init` and nothing else writes into them, so the merge replaces them
// whole. The three [MCPServers.gintrack] auth branches (encrypted Auth,
// plaintext Headers, commented-out stub) are mutually exclusive, and replacing
// the group as a unit is what guarantees only one of them survives a re-run
// that changes the token mode.
func agentOwnedTables(persona string) []string {
	return []string{
		"AGUI.Profiles." + persona,
		"MCPServers.gintrack",
		"MCPServers.gintrack.Auth",
		"MCPServers.gintrack.Headers",
	}
}

// tomlSection is one table of a TOML file as text: the comment and blank lines
// that introduce it, its header line, and its body up to the next header. The
// first section of a file has no header and holds the root-level keys.
type tomlSection struct {
	name   string   // the table path, "" for the root section
	lead   []string // comments and blank lines before the header
	header string   // the header line verbatim, "" for the root section
	body   []string // every line after the header, verbatim
}

// lines renders a section back exactly as it was read.
func (s tomlSection) lines() []string {
	out := make([]string, 0, len(s.lead)+1+len(s.body))
	out = append(out, s.lead...)
	if s.header != "" {
		out = append(out, s.header)
	}
	return append(out, s.body...)
}

// tomlHeaderPattern matches a table header line, `[table]` or `[[array]]`,
// optionally followed by a comment.
var tomlHeaderPattern = regexp.MustCompile(`^(\[\[?)([^\[\]]+)(\]\]?)\s*(#.*)?$`)

// parsePandoTOML splits a file into sections. It only ever looks at the lines
// it needs to recognize — everything it does not understand is carried through
// verbatim — but a line that opens a table and cannot be parsed is an error
// rather than a guess, because guessing would move a user's keys into the wrong
// table.
func parsePandoTOML(text string) ([]tomlSection, error) {
	lines := strings.Split(text, "\n")
	// A trailing newline produces a final empty element; drop it and restore it
	// when the file is rendered.
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1]
	}

	sections := []tomlSection{{}}
	depth := 0
	for i, line := range lines {
		current := &sections[len(sections)-1]
		trimmed := strings.TrimSpace(line)
		if depth == 0 && strings.HasPrefix(trimmed, "[") {
			m := tomlHeaderPattern.FindStringSubmatch(trimmed)
			if m == nil {
				return nil, fmt.Errorf("line %d opens a table but cannot be parsed: %q", i+1, line)
			}
			if len(m[1]) != len(m[3]) {
				return nil, fmt.Errorf("line %d opens a table but cannot be parsed: %q", i+1, line)
			}
			name := strings.TrimSpace(m[2])
			if name == "" {
				return nil, fmt.Errorf("line %d opens a table but cannot be parsed: %q", i+1, line)
			}
			lead := takeLead(current)
			sections = append(sections, tomlSection{
				name:   m[1] + name, // "[[x]]" keeps its marker so arrays never match a table
				lead:   lead,
				header: line,
			})
			continue
		}
		current.body = append(current.body, line)
		depth += tomlBracketDelta(line)
		if depth < 0 {
			depth = 0
		}
	}
	return sections, nil
}

// takeLead moves the comment and blank lines that trail a section's body out of
// it, because they introduce the header that follows rather than close the
// table before it.
func takeLead(s *tomlSection) []string {
	// The comment block that sits directly on top of the header introduces it;
	// the blank lines above that block separate it from what came before. A
	// comment paragraph further up, already separated by a blank line, belongs
	// to the table that precedes it — which is what keeps a generated file's
	// own preamble out of the lead of its first table.
	cut := len(s.body)
	for cut > 0 && strings.HasPrefix(strings.TrimSpace(s.body[cut-1]), "#") {
		cut--
	}
	for cut > 0 && strings.TrimSpace(s.body[cut-1]) == "" {
		cut--
	}
	lead := append([]string(nil), s.body[cut:]...)
	s.body = s.body[:cut]
	return lead
}

// tomlBracketDelta counts the brackets and braces a line opens minus the ones
// it closes, ignoring anything inside a string. It is what tells a multi-line
// array's continuation lines apart from a table header.
func tomlBracketDelta(line string) int {
	delta := 0
	var quote byte
	for i := 0; i < len(line); i++ {
		c := line[i]
		if quote != 0 {
			if quote == '"' && c == '\\' {
				i++
				continue
			}
			if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '\'', '"':
			quote = c
		case '#':
			return delta
		case '[', '{':
			delta++
		case ']', '}':
			delta--
		}
	}
	return delta
}

// tomlKeyPattern matches the start of a bare key assignment.
var tomlKeyPattern = regexp.MustCompile(`^([A-Za-z0-9_-]+)\s*=`)

// findTOMLKey returns the half-open line range a key occupies in a body, and
// the range of the comment lines immediately above it. It returns start < 0
// when the key is absent.
func findTOMLKey(body []string, key string) (start, end, comment int) {
	depth := 0
	for i := 0; i < len(body); i++ {
		if depth > 0 {
			depth += tomlBracketDelta(body[i])
			if depth < 0 {
				depth = 0
			}
			continue
		}
		trimmed := strings.TrimSpace(body[i])
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		m := tomlKeyPattern.FindStringSubmatch(trimmed)
		if m == nil {
			continue
		}
		depth = tomlBracketDelta(body[i])
		if depth < 0 {
			depth = 0
		}
		if m[1] != key {
			continue
		}
		start = i
		end = i + 1
		for depth > 0 && end < len(body) {
			depth += tomlBracketDelta(body[end])
			end++
		}
		comment = start
		for comment > 0 && strings.HasPrefix(strings.TrimSpace(body[comment-1]), "#") {
			comment--
		}
		return start, end, comment
	}
	return -1, -1, -1
}

// tomlKeyValue is the text of a key's value, with the key name and the equals
// sign removed, normalized enough that two spellings of the same value compare
// equal: the quote style tomlString picks, the layout of a multi-line array and
// the trailing comma TOML allows are all presentation.
func tomlKeyValue(body []string, start, end int) string {
	joined := strings.Join(body[start:end], " ")
	if idx := strings.Index(joined, "="); idx >= 0 {
		joined = joined[idx+1:]
	}
	return normalizeTOMLValue(joined)
}

// normalizeTOMLValue collapses whitespace outside strings, rewrites literal
// strings as basic ones and drops a trailing comma before a closing bracket.
func normalizeTOMLValue(value string) string {
	var b strings.Builder
	var quote byte
	space := false
	for i := 0; i < len(value); i++ {
		c := value[i]
		if quote != 0 {
			if quote == '"' && c == '\\' && i+1 < len(value) {
				b.WriteByte(c)
				i++
				b.WriteByte(value[i])
				continue
			}
			if c == quote {
				quote = 0
				b.WriteByte('"')
				continue
			}
			if quote == '\'' && c == '"' {
				// A double quote inside a literal string has to be escaped once
				// the string is rewritten as a basic one.
				b.WriteString(`\"`)
				continue
			}
			b.WriteByte(c)
			continue
		}
		switch c {
		case '\'', '"':
			quote = c
			b.WriteByte('"')
			space = false
		case '#':
			i = len(value)
		case ' ', '\t', '\r':
			space = true
		default:
			if space && b.Len() > 0 && c != ']' && c != '}' && c != ',' {
				b.WriteByte(' ')
			}
			space = false
			b.WriteByte(c)
		}
	}
	out := b.String()
	out = strings.ReplaceAll(out, "[ ", "[")
	out = strings.ReplaceAll(out, ",]", "]")
	out = strings.ReplaceAll(out, ",}", "}")
	return strings.TrimSpace(out)
}

// mergePandoTOML folds the rendered template into an existing configuration.
//
// Tables gintrack owns outright are replaced whole, at the position the first
// of them had. Tables it shares with the user keep every key, comment and blank
// line they had: a key gintrack owns and the table lacks is inserted with the
// template's comment above it, and a key that is already there with another
// value is left alone and reported. Tables the file does not have at all are
// appended verbatim, comments included.
func mergePandoTOML(existing, rendered, persona string) (string, []agentDivergence, error) {
	have, err := parsePandoTOML(existing)
	if err != nil {
		return "", nil, fmt.Errorf("%s: %w", agentPandoConfigName, err)
	}
	want, err := parsePandoTOML(rendered)
	if err != nil {
		return "", nil, fmt.Errorf("the rendered %s template: %w", agentPandoConfigName, err)
	}

	owned := make(map[string]bool, 4)
	for _, name := range agentOwnedTables(persona) {
		owned["["+name] = true
	}

	// Lift the owned tables out of the existing file, remembering where the
	// first of them sat so the replacement lands in the same place.
	anchor := -1
	kept := make([]tomlSection, 0, len(have))
	for _, s := range have {
		if owned[s.name] {
			if anchor < 0 {
				anchor = len(kept)
			}
			continue
		}
		kept = append(kept, s)
	}

	var divergences []agentDivergence
	index := make(map[string]int, len(kept))
	for i, s := range kept {
		if s.name != "" {
			if _, seen := index[s.name]; !seen {
				index[s.name] = i
			}
		}
	}

	var replacement, appended []tomlSection
	for _, s := range want {
		if s.name == "" {
			continue // the template's preamble is its own header comment
		}
		if owned[s.name] {
			replacement = append(replacement, s)
			continue
		}
		at, ok := index[s.name]
		if !ok {
			appended = append(appended, s)
			continue
		}
		merged, divs := mergeSectionKeys(kept[at], s)
		kept[at] = merged
		divergences = append(divergences, divs...)
	}

	if anchor < 0 {
		anchor = len(kept)
	}
	out := make([]tomlSection, 0, len(kept)+len(replacement)+len(appended))
	out = append(out, kept[:anchor]...)
	out = append(out, replacement...)
	out = append(out, kept[anchor:]...)
	out = append(out, appended...)

	var lines []string
	for _, s := range out {
		lines = append(lines, s.lines()...)
	}
	// Anything appended after the end of the file needs a blank line in front
	// of it when the file did not end with one; the template's own leads carry
	// a blank line, so this only fires for a file that had none.
	text := strings.Join(lines, "\n")
	text = strings.TrimRight(text, "\n") + "\n"
	return text, divergences, nil
}

// aguiTable is the one shared table where a key left at its zero value counts
// as unset rather than as a decision. Pando writes `[AGUI]` out with every key
// at its zero value the moment anything touches the section, so `Enabled =
// false` / `Path = ”` / `Port = 0` there is the absence of a choice, not a
// choice — and a merge that only warned about them would leave the adapter off
// and the panel broken. The rule stops at this table on purpose: `false` in
// [ToolDiscovery], [MCPGateway] or [MCPServer] is a setting someone relies on.
const aguiTable = "AGUI"

// isZeroTOMLValue reports whether a normalized value is the zero of its type.
func isZeroTOMLValue(value string) bool {
	switch value {
	case `""`, "0", "0.0", "false", "[]", "{}":
		return true
	}
	return false
}

// mergeSectionKeys writes the keys gintrack owns into a table it shares with
// the user: absent keys are added with their comment, keys that already agree
// are left as they are, and keys that disagree are reported — except for the
// [AGUI] zero values above, which are written over in place.
func mergeSectionKeys(into, from tomlSection) (tomlSection, []agentDivergence) {
	table := strings.TrimPrefix(into.name, "[")
	var divergences []agentDivergence
	body := append([]string(nil), into.body...)

	for _, key := range agentOwnedKeys[table] {
		wantStart, wantEnd, wantComment := findTOMLKey(from.body, key)
		if wantStart < 0 {
			continue // the template did not render this key in this run
		}
		wantValue := tomlKeyValue(from.body, wantStart, wantEnd)

		haveStart, haveEnd, haveComment := findTOMLKey(body, key)
		if haveStart >= 0 {
			if tomlKeyValue(body, haveStart, haveEnd) == wantValue {
				continue
			}
			if table == aguiTable && isZeroTOMLValue(tomlKeyValue(body, haveStart, haveEnd)) {
				// Rewritten in place, keeping the user's ordering. The template's
				// comment comes along only when the key has none of its own, so a
				// note the user wrote is never displaced or duplicated.
				replace := from.body[wantStart:wantEnd]
				if haveComment == haveStart {
					replace = from.body[wantComment:wantEnd]
				}
				body = append(body[:haveStart], append(append([]string(nil), replace...), body[haveEnd:]...)...)
				continue
			}
			divergences = append(divergences, agentDivergence{
				Table:       table,
				Key:         key,
				Have:        strings.TrimSpace(strings.Join(body[haveStart:haveEnd], " ")),
				Recommended: strings.TrimSpace(strings.Join(from.body[wantStart:wantEnd], " ")),
				Reason:      agentDivergenceReason(table, key),
			})
			continue
		}

		// Insert after the last line that is neither blank nor a comment, so a
		// trailing comment of the user's keeps its position.
		at := len(body)
		for at > 0 {
			t := strings.TrimSpace(body[at-1])
			if t == "" || strings.HasPrefix(t, "#") {
				at--
				continue
			}
			break
		}
		insert := append([]string(nil), from.body[wantComment:wantEnd]...)
		body = append(body[:at], append(insert, body[at:]...)...)
	}
	into.body = body
	return into, divergences
}

// agentDivergenceReason is the one-line reason printed for a key the merge left
// alone: the key's own explanation when it has one, the table's otherwise.
func agentDivergenceReason(table, key string) string {
	if reason, ok := agentDivergenceReasons[table+"."+key]; ok {
		return reason
	}
	if reason, ok := agentDivergenceReasons[table]; ok {
		return reason
	}
	return "the generated configuration expects this value"
}

// agentMergePandoConfig merges the rendered configuration into the file that is
// already there, after copying it aside. `.pando.toml` is gitignored, so git is
// not a safety net and the backup is the only way back.
func agentMergePandoConfig(dest string, rendered []byte, persona string) (string, []agentDivergence, error) {
	current, err := os.ReadFile(dest)
	if err != nil {
		return "", nil, fmt.Errorf("read %s: %w", dest, err)
	}
	merged, divergences, err := mergePandoTOML(string(current), string(rendered), persona)
	if err != nil {
		// Nothing has been written at this point: no backup, no edit.
		return "", nil, failf(exitConflict, "%v; nothing was written", err)
	}
	backup := fmt.Sprintf("%s.%s.bak", dest, time.Now().UTC().Format("20060102T150405Z"))
	if err := os.WriteFile(backup, current, 0o600); err != nil {
		return "", nil, fmt.Errorf("write %s: %w", backup, err)
	}
	if err := os.WriteFile(dest, []byte(merged), 0o600); err != nil {
		return "", nil, fmt.Errorf("write %s: %w", dest, err)
	}
	if err := os.Chmod(dest, 0o600); err != nil {
		return "", nil, fmt.Errorf("set the mode of %s: %w", dest, err)
	}
	return backup, divergences, nil
}
