package trace

import (
	"path"
	"strings"

	"github.com/digiogithub/git-in-track/internal/core"
)

// syntax is the set of comment syntaxes a file type uses (ADR-037 section 8
// table). A file type may use several: .vue and .svelte use both the C-like
// row and the HTML row.
type syntax uint8

const (
	synSlash syntax = 1 << iota // "//" line comments and "/* */" block comments
	synHash                     // "#" line comments
	synDash                     // "--" line comments
	synHTML                     // "<!-- -->" block comments
)

// language selects the symbol attribution strategy of a file type.
type language uint8

const (
	langNone   language = iota // markers attach to the whole file
	langGo                     // go/parser declarations
	langJS                     // JS/TS line heuristic
	langPython                 // indentation heuristic
)

// fileType is what the scanner knows about one file type.
type fileType struct {
	syntax   syntax
	lang     language
	markdown bool // fenced code blocks are skipped
}

// extTypes maps a lower-cased extension to its file type. Adding a row is an
// additive scanner change, not a data-model change (R-MARK-4).
var extTypes = map[string]fileType{}

// nameTypes maps a base file name that has no meaningful extension.
var nameTypes = map[string]fileType{
	"Makefile":   {syntax: synHash},
	"Dockerfile": {syntax: synHash},
}

func init() {
	for _, e := range []string{".java", ".kt", ".swift", ".c", ".h", ".cc", ".cpp", ".hpp",
		".cs", ".rs", ".scala", ".dart", ".php", ".css", ".scss"} {
		extTypes[e] = fileType{syntax: synSlash}
	}
	extTypes[".go"] = fileType{syntax: synSlash, lang: langGo}
	for _, e := range []string{".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs"} {
		extTypes[e] = fileType{syntax: synSlash, lang: langJS}
	}
	for _, e := range []string{".rb", ".sh", ".bash", ".zsh", ".pl", ".r", ".yaml", ".yml", ".toml", ".tf"} {
		extTypes[e] = fileType{syntax: synHash}
	}
	extTypes[".py"] = fileType{syntax: synHash, lang: langPython}
	extTypes[".sql"] = fileType{syntax: synDash}
	extTypes[".lua"] = fileType{syntax: synDash}
	for _, e := range []string{".html", ".htm", ".xml", ".svg"} {
		extTypes[e] = fileType{syntax: synHTML}
	}
	extTypes[".md"] = fileType{syntax: synHTML, markdown: true}
	extTypes[".vue"] = fileType{syntax: synSlash | synHTML}
	extTypes[".svelte"] = fileType{syntax: synSlash | synHTML}
}

// typeOf returns the file type of a repository path, and false when the file
// is not scanned at all.
func typeOf(p string) (fileType, bool) {
	base := path.Base(p)
	if ft, ok := nameTypes[base]; ok {
		return ft, true
	}
	ft, ok := extTypes[strings.ToLower(path.Ext(base))]
	return ft, ok
}

// Scannable reports whether a repository path has a file type the marker
// scanner reads (docs/03 section 21.7 table).
func Scannable(p string) bool {
	_, ok := typeOf(p)
	return ok
}

// block is the kind of block comment open at the start of a line.
type block uint8

const (
	blockNone block = iota
	blockC          // inside "/* ... */"
	blockHTML       // inside "<!-- ... -->"
)

// lineResult is the outcome of matching one line against the marker grammar.
type lineResult uint8

const (
	lineNone      lineResult = iota // not a marker
	lineMarker                      // a well-formed marker
	lineMalformed                   // opener and keyword match, the ref list does not: W-MARKER-SYNTAX
)

// markerRef is one ref of a marker: the optional project qualifier and the
// requirement ref.
type markerRef struct {
	Project core.ProjectKey
	Ref     core.RequirementRef
}

// Keywords of the marker grammar, case-sensitive and exact.
const (
	kwImplements = "Implements"
	kwVerifies   = "Verifies"
)

// openersFor returns the openers accepted on a line, longest first so that
// "/**" is tried before "/*", given the syntaxes of the file type and the
// block comment already open at the start of the line.
func openersFor(syn syntax, open block) []string {
	switch open {
	case blockC:
		return []string{"*", ""}
	case blockHTML:
		return []string{""}
	}
	var out []string
	if syn&synSlash != 0 {
		out = append(out, "/**", "/*", "//")
	}
	if syn&synHTML != 0 {
		out = append(out, "<!--")
	}
	if syn&synDash != 0 {
		out = append(out, "--")
	}
	if syn&synHash != 0 {
		out = append(out, "#")
	}
	return out
}

// closerFor returns the comment closer allowed at the end of a marker line
// opened by op in the given block state, or "" when none is.
func closerFor(op string, open block) string {
	switch {
	case open == blockC, op == "/*", op == "/**":
		return "*/"
	case open == blockHTML, op == "<!--":
		return "-->"
	}
	return ""
}

// matchLine applies the exact match rule of ADR-037 section 8 (R-MARK-1) to
// one whole source line.
func matchLine(line string, syn syntax, open block) (Kind, []markerRef, lineResult) {
	s := strings.TrimLeft(line, " \t")
	for _, op := range openersFor(syn, open) {
		rest, ok := strings.CutPrefix(s, op)
		if !ok {
			continue
		}
		rest = strings.TrimLeft(rest, " \t")
		var kind Kind
		switch {
		case strings.HasPrefix(rest, kwImplements+":"):
			kind, rest = KindImplements, rest[len(kwImplements)+1:]
		case strings.HasPrefix(rest, kwVerifies+":"):
			kind, rest = KindVerifies, rest[len(kwVerifies)+1:]
		default:
			continue
		}
		refs, ok := parseRefList(rest, closerFor(op, open))
		if !ok {
			return kind, nil, lineMalformed
		}
		return kind, refs, lineMarker
	}
	return "", nil, lineNone
}

// parseRefList parses what follows "<kw>:": at least one whitespace, one or
// more comma-separated refs, one optional trailing comma and the closer.
func parseRefList(rest, closer string) ([]markerRef, bool) {
	if rest == "" || (rest[0] != ' ' && rest[0] != '\t') {
		return nil, false
	}
	rest = strings.TrimRight(rest, " \t\r")
	if closer != "" {
		rest = strings.TrimSuffix(rest, closer)
	}
	rest = strings.TrimSpace(rest)
	rest = strings.TrimSuffix(rest, ",")
	if strings.TrimSpace(rest) == "" {
		return nil, false
	}
	parts := strings.Split(rest, ",")
	refs := make([]markerRef, 0, len(parts))
	for _, p := range parts {
		r, ok := parseMarkerRef(strings.TrimSpace(p))
		if !ok {
			return nil, false
		}
		refs = append(refs, r)
	}
	return refs, true
}

// parseMarkerRef parses "[<KEY>/]<REQREF>" as a whole token, reusing the ref
// grammar of internal/core.
func parseMarkerRef(tok string) (markerRef, bool) {
	var key core.ProjectKey
	if k, rest, ok := strings.Cut(tok, "/"); ok {
		key = core.ProjectKey(k)
		if !core.ValidProjectKey(key) {
			return markerRef{}, false
		}
		tok = rest
	}
	ref, err := core.ParseRequirementRef(tok)
	if err != nil {
		return markerRef{}, false
	}
	return markerRef{Project: key, Ref: ref}, true
}

// nextBlockState returns the block comment open at the end of a line, given
// the one open at its start. It is a heuristic for the non-Go file types:
// quoted strings are skipped only in purely C-like files, where "/*" inside a
// string literal is common; a C-like "//" ends the scan of the line.
func nextBlockState(line string, syn syntax, open block) block {
	if syn&(synSlash|synHTML) == 0 {
		return blockNone
	}
	skipQuotes := syn == synSlash
	i := 0
	for i < len(line) {
		switch open {
		case blockC:
			j := strings.Index(line[i:], "*/")
			if j < 0 {
				return open
			}
			open, i = blockNone, i+j+2
			continue
		case blockHTML:
			j := strings.Index(line[i:], "-->")
			if j < 0 {
				return open
			}
			open, i = blockNone, i+j+3
			continue
		}
		c := line[i]
		switch {
		case skipQuotes && (c == '"' || c == '\'' || c == '`'):
			i = skipQuoted(line, i)
		case syn&synSlash != 0 && strings.HasPrefix(line[i:], "//"):
			return blockNone
		case syn&synSlash != 0 && strings.HasPrefix(line[i:], "/*"):
			open, i = blockC, i+2
		case syn&synHTML != 0 && strings.HasPrefix(line[i:], "<!--"):
			open, i = blockHTML, i+4
		default:
			i++
		}
	}
	return open
}

// skipQuoted returns the index after the string literal that starts at i, or
// the end of the line when it is not closed there.
func skipQuoted(line string, i int) int {
	q := line[i]
	for j := i + 1; j < len(line); j++ {
		switch line[j] {
		case '\\':
			j++
		case q:
			return j + 1
		}
	}
	return len(line)
}

// isCommentLine reports whether a line holds nothing but comment text: it is
// inside an open block comment or starts with an opener of the file type.
func isCommentLine(line string, syn syntax, open block) bool {
	if open != blockNone {
		return true
	}
	s := strings.TrimLeft(line, " \t")
	for _, op := range openersFor(syn, blockNone) {
		if strings.HasPrefix(s, op) {
			return true
		}
	}
	return false
}
