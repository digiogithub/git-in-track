package trace

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strconv"
	"strings"
)

// The symbol spans of a file, for the trace graph: which symbol encloses a
// given line (a diff's changed lines become changed symbols), and which
// symbols a file declares (a trace: entry naming one that is gone is broken).
// Both use exactly the attribution rules of the marker scan (R-MARK-2), so a
// symbol the graph compares is always spelled the way a marker spells it.

// SymbolsAt returns the symbol enclosing each of the given 1-based lines of a
// file, "" for a line outside every declaration or in a file type without
// declarations. A Go file that does not parse maps every line to "".
func SymbolsAt(p string, src []byte, lines []int) map[int]string {
	out := make(map[int]string, len(lines))
	ft, ok := typeOf(p)
	if !ok || len(lines) == 0 {
		for _, ln := range lines {
			out[ln] = ""
		}
		return out
	}
	if ft.lang == langGo {
		decls := goDecls(p, src)
		for _, ln := range lines {
			out[ln] = decls.symbolAt(ln)
		}
		return out
	}
	all := lineSymbols(ft, splitLines(src))
	for _, ln := range lines {
		if ln >= 1 && ln <= len(all) {
			out[ln] = all[ln-1]
		} else {
			out[ln] = ""
		}
	}
	return out
}

// lineSymbols runs the heuristic symbolizer of a non-Go file type over every
// line and returns the enclosing symbol of each. A blank line belongs to the
// symbol of the line before it.
func lineSymbols(ft fileType, lines []string) []string {
	out := make([]string, len(lines))
	sym := newSymbolizer(ft.lang)
	open := blockNone
	prev := ""
	for i, line := range lines {
		switch {
		case sym.inLiteral():
			sym.step(line)
			out[i] = sym.enclosing(line)
		case strings.TrimSpace(line) == "":
			out[i] = prev
			continue
		case isCommentLine(line, ft.syntax, open) || sym.transparent(line):
			open = nextBlockState(line, ft.syntax, open)
			out[i] = sym.enclosing(line)
		default:
			open = nextBlockState(line, ft.syntax, open)
			if decl, ok := sym.step(line); ok {
				out[i] = decl
			} else {
				out[i] = sym.enclosing(line)
			}
		}
		prev = out[i]
	}
	return out
}

// DeclaredSymbols returns every symbol a file declares, in the trace-ref
// spelling. ok is false when the file's symbols cannot be known: a file type
// without declarations, or a Go file that does not parse. A trace: entry into
// such a file is never reported broken for its symbol.
func DeclaredSymbols(p string, src []byte) (symbols []string, ok bool) {
	ft, known := typeOf(p)
	if !known || ft.lang == langNone {
		return nil, false
	}
	set := map[string]bool{}
	if ft.lang == langGo {
		if !goSymbols(p, src, set) {
			return nil, false
		}
	} else {
		sym := newSymbolizer(ft.lang)
		open := blockNone
		for _, line := range splitLines(src) {
			if sym.inLiteral() {
				sym.step(line)
				continue
			}
			comment := isCommentLine(line, ft.syntax, open)
			open = nextBlockState(line, ft.syntax, open)
			if comment || strings.TrimSpace(line) == "" || sym.transparent(line) {
				continue
			}
			if decl, isDecl := sym.step(line); isDecl {
				set[decl] = true
			}
		}
	}
	for s := range set {
		symbols = append(symbols, s)
	}
	sort.Strings(symbols)
	return symbols, true
}

// goSymbols adds the declarations of a Go file and the t.Run sub-test paths
// of its functions. It reports false when the file does not parse.
func goSymbols(p string, src []byte, set map[string]bool) bool {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, p, src, parser.SkipObjectResolution)
	if f == nil || err != nil {
		return false
	}
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			name := funcName(d)
			set[name] = true
			if d.Body != nil {
				subTests(d.Body, name, set)
			}
		case *ast.GenDecl:
			for _, s := range d.Specs {
				if n := specName(s); n != "" {
					set[n] = true
				}
			}
		}
	}
	return true
}

// subTests adds "<prefix>/<name>" for every literal t.Run name below n,
// nested the way go test names sub-tests.
func subTests(n ast.Node, prefix string, set map[string]bool) {
	ast.Inspect(n, func(x ast.Node) bool {
		call, ok := x.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Run" || len(call.Args) != 2 {
			return true
		}
		lit, ok := call.Args[0].(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		name, err := strconv.Unquote(lit.Value)
		if err != nil {
			return true
		}
		sub := prefix + "/" + strings.ReplaceAll(name, " ", "_")
		set[sub] = true
		subTests(call.Args[1], sub, set)
		return false
	})
}

// symbolSeps are the separators of a symbol path: Type.Method, TestX/sub and
// "describe > it".
var symbolSeps = []string{".", "/", " > "}

// symbolsOverlap reports whether two symbols name the same code or one
// encloses the other: a change inside TestX/sub touches a trace to TestX, and
// a change to TestX outside its sub-tests touches a trace to TestX/sub.
func symbolsOverlap(a, b string) bool {
	if a == b {
		return true
	}
	if len(a) > len(b) {
		a, b = b, a
	}
	for _, sep := range symbolSeps {
		if strings.HasPrefix(b, a+sep) {
			return true
		}
	}
	return false
}
