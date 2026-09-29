package impact

import (
	"io/fs"
	"strings"
	"unicode"

	"github.com/digiogithub/git-in-track/internal/core"
	"github.com/digiogithub/git-in-track/internal/trace"
)

// maxQueryBytes caps the tier-3 query text. Pando embeds the query whole and
// its full-text leg requires every word, so a longer query buys nothing.
const maxQueryBytes = 1000

// queryText is the tier-3 query (GIT-US-0165, docs/03 R-IMP-4), one part per
// line in this order:
//
//   - the story title, then each operation of its `## Spec Delta` as
//     "<title>: <statement>" (a REMOVED one with its Reason:) — the words of
//     the requirement the story means to change;
//   - the caller's title;
//   - each changed declaration in words, at most maxQueryNames in sorted
//     order: the first sentence of its Go doc comment when it has one, else
//     its name split into lower-case words ("nextNumber" is "next number").
//     Declarations in test files count only when nothing else changed.
//
// Bare identifiers match prose about the code (docs pages, stories,
// comments) better than the EARS statement of a requirement; a sentence
// saying what the code does ranks near the statement that says what it
// must do. The text is cut at maxQueryBytes, on a word boundary.
func queryText(ix *core.Index, q core.ImpactQuery, symbols []changedSymbol, tree fs.FS) string {
	var parts []string
	if q.Story != "" {
		if it, err := ix.Item(q.Story); err == nil {
			parts = append(parts, strings.TrimSpace(it.Title))
			parts = append(parts, deltaParts(it.Body)...)
		}
	}
	if t := strings.TrimSpace(q.Title); t != "" {
		parts = append(parts, t)
	}
	parts = append(parts, declParts(symbols, tree)...)
	var kept []string
	for _, p := range parts {
		if p = strings.Join(strings.Fields(p), " "); p != "" {
			kept = append(kept, p)
		}
	}
	return capWords(strings.Join(kept, "\n"), maxQueryBytes)
}

// deltaParts renders the operations of a body's Spec Delta.
func deltaParts(body string) []string {
	var out []string
	for _, op := range core.ParseSpecDelta(body).Operations {
		text := op.Statement
		if op.Op == core.DeltaRemoved {
			text = op.Reason
		}
		title, text := strings.TrimSpace(op.Title), strings.TrimSpace(text)
		switch {
		case title != "" && text != "":
			out = append(out, title+": "+text)
		case title != "":
			out = append(out, title)
		case text != "":
			out = append(out, text)
		}
	}
	return out
}

// declParts renders the changed declarations in words, test files only when
// nothing else changed. symbols is sorted by path and symbol.
func declParts(symbols []changedSymbol, tree fs.FS) []string {
	var code, tests []changedSymbol
	for _, s := range symbols {
		if isTestPath(s.path) {
			tests = append(tests, s)
		} else {
			code = append(code, s)
		}
	}
	if len(code) == 0 {
		code = tests
	}
	docs := map[string]map[string]string{}
	seen := map[string]bool{}
	var out []string
	for _, s := range code {
		if len(out) == maxQueryNames {
			break
		}
		name := pandoName(s.symbol)
		if name == "" {
			continue
		}
		text := ""
		if tree != nil {
			d, ok := docs[s.path]
			if !ok {
				if src, err := fs.ReadFile(tree, s.path); err == nil {
					d = trace.DocSummaries(s.path, src)
				}
				docs[s.path] = d
			}
			// The doc is keyed by the declaration, "Type.Method" included;
			// a sub-test path is its test function's.
			decl, _, _ := strings.Cut(s.symbol, "/")
			text = d[decl]
		}
		if text == "" {
			if isTestPath(s.path) {
				name = strings.TrimPrefix(name, "Test")
			}
			text = splitIdentifier(name)
		}
		if text == "" || seen[text] {
			continue
		}
		seen[text] = true
		out = append(out, text)
	}
	return out
}

// splitIdentifier turns an identifier into lower-case words: camelCase,
// PascalCase, acronyms and snake_case ("HTTPServer" is "http server").
func splitIdentifier(id string) string {
	rs := []rune(id)
	var b strings.Builder
	for i, r := range rs {
		if r == '_' || r == '-' {
			if b.Len() > 0 && !strings.HasSuffix(b.String(), " ") {
				b.WriteByte(' ')
			}
			continue
		}
		if i > 0 && unicode.IsUpper(r) && b.Len() > 0 && !strings.HasSuffix(b.String(), " ") {
			prev := rs[i-1]
			nextLower := i+1 < len(rs) && unicode.IsLower(rs[i+1])
			if !unicode.IsUpper(prev) || nextLower {
				b.WriteByte(' ')
			}
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return strings.TrimSpace(b.String())
}

// capWords cuts s to at most n bytes at the last whitespace, so no word is
// cut in half.
func capWords(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := s[:n+1]
	if i := strings.LastIndexAny(cut, " \n"); i > 0 {
		return strings.TrimSpace(cut[:i])
	}
	return s[:n]
}
