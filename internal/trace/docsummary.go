package trace

import (
	"go/ast"
	"go/doc"
	"go/parser"
	"go/token"
	"strings"
)

// DocSummaries returns the first sentence of the doc comment of every named
// top-level declaration of a Go file, keyed by the symbol a marker would
// attach to (R-MARK-2): "Func", "Type.Method", or the name of a type, a
// constant or a variable — a spec of a grouped declaration by its own doc.
// Marker lines (Implements:, Verifies:) are not prose and are dropped first;
// a declaration whose doc holds nothing else is absent.
//
// It feeds the tier-3 impact query (GIT-US-0165): a doc comment describes
// what a declaration does in words, which ranks near the statement of a
// requirement better than the bare identifier does. Only Go is read; any
// other file, or a Go file that does not parse, returns nil.
func DocSummaries(p string, src []byte) map[string]string {
	ft, ok := typeOf(p)
	if !ok || ft.lang != langGo {
		return nil
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, p, src, parser.ParseComments|parser.SkipObjectResolution)
	if f == nil || err != nil {
		return nil
	}
	var out map[string]string
	add := func(name string, cg *ast.CommentGroup) {
		if name == "" || cg == nil {
			return
		}
		if s := docSentence(cg.Text()); s != "" {
			if out == nil {
				out = map[string]string{}
			}
			out[name] = s
		}
	}
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			add(funcName(d), d.Doc)
		case *ast.GenDecl:
			for _, s := range d.Specs {
				cg := specDoc(s)
				if cg == nil && len(d.Specs) == 1 {
					cg = d.Doc
				}
				add(specName(s), cg)
			}
		}
	}
	return out
}

// docSentence is the first sentence of a doc comment's text, with the
// marker lines removed and the lines joined, by go/doc's own rule.
func docSentence(text string) string {
	var kept []string
	for _, l := range strings.Split(text, "\n") {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "Implements:") || strings.HasPrefix(t, "Verifies:") {
			continue
		}
		kept = append(kept, l)
	}
	return strings.TrimSpace(new(doc.Package).Synopsis(strings.Join(kept, "\n")))
}
