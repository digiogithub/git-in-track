package trace

import (
	"go/ast"
	"go/parser"
	"go/token"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// ---- Go: declarations from go/parser ----

// goDecl is the line extent of one top-level declaration and of the doc
// comment directly above it.
type goDecl struct {
	name             string
	docStart, docEnd int // 0 when there is no doc comment
	start, end       int
	body             *ast.FuncDecl // for t.Run sub-test names
	specs            []goSpec      // the specs of a parenthesised GenDecl
	fset             *token.FileSet
}

// goSpec is one spec of a grouped declaration, "var ( a = 1; b = 2 )".
type goSpec struct {
	name                         string
	docStart, docEnd, start, end int
}

type goDeclSet struct {
	decls []goDecl
}

// goDecls parses a Go file for its declarations. A file that does not parse
// keeps whatever declarations the parser recovered; with none, every marker
// attaches to the whole file.
func goDecls(p string, src []byte) goDeclSet {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, p, src, parser.ParseComments|parser.SkipObjectResolution)
	if f == nil || err != nil {
		// A file that does not parse (mid-edit) has no reliable declaration
		// extents; its markers attach to the whole file.
		return goDeclSet{}
	}
	line := func(pos token.Pos) int { return fset.Position(pos).Line }
	var out goDeclSet
	for _, d := range f.Decls {
		gd := goDecl{start: line(d.Pos()), end: line(d.End()), fset: fset}
		switch d := d.(type) {
		case *ast.FuncDecl:
			gd.name = funcName(d)
			gd.body = d
			if d.Doc != nil {
				gd.docStart, gd.docEnd = line(d.Doc.Pos()), line(d.Doc.End())
			}
		case *ast.GenDecl:
			if d.Doc != nil {
				gd.docStart, gd.docEnd = line(d.Doc.Pos()), line(d.Doc.End())
			}
			for _, s := range d.Specs {
				sp := goSpec{name: specName(s), start: line(s.Pos()), end: line(s.End())}
				if doc := specDoc(s); doc != nil {
					sp.docStart, sp.docEnd = line(doc.Pos()), line(doc.End())
				}
				gd.specs = append(gd.specs, sp)
			}
			if len(gd.specs) == 1 {
				gd.name = gd.specs[0].name
			}
		}
		out.decls = append(out.decls, gd)
	}
	return out
}

// symbolAt returns the symbol a marker on the given line attaches to
// (R-MARK-2), or "" for the whole file.
func (s goDeclSet) symbolAt(ln int) string {
	for _, d := range s.decls {
		if d.docStart > 0 && ln >= d.docStart && ln <= d.docEnd {
			return d.name
		}
		if ln < d.start || ln > d.end {
			continue
		}
		for _, sp := range d.specs {
			if (sp.docStart > 0 && ln >= sp.docStart && ln <= sp.docEnd) || (ln >= sp.start && ln <= sp.end) {
				return sp.name
			}
		}
		if d.body != nil {
			return d.name + subTestPath(d.body, d.fset, ln)
		}
		return d.name
	}
	return ""
}

// funcName renders "Func" or "Type.Method".
func funcName(d *ast.FuncDecl) string {
	if d.Recv == nil || len(d.Recv.List) == 0 {
		return d.Name.Name
	}
	t := d.Recv.List[0].Type
	for {
		switch x := t.(type) {
		case *ast.StarExpr:
			t = x.X
			continue
		case *ast.IndexExpr:
			t = x.X
			continue
		case *ast.IndexListExpr:
			t = x.X
			continue
		case *ast.ParenExpr:
			t = x.X
			continue
		case *ast.Ident:
			return x.Name + "." + d.Name.Name
		}
		return d.Name.Name
	}
}

func specName(s ast.Spec) string {
	switch s := s.(type) {
	case *ast.TypeSpec:
		return s.Name.Name
	case *ast.ValueSpec:
		if len(s.Names) > 0 {
			return s.Names[0].Name
		}
	}
	return ""
}

func specDoc(s ast.Spec) *ast.CommentGroup {
	switch s := s.(type) {
	case *ast.TypeSpec:
		return s.Doc
	case *ast.ValueSpec:
		return s.Doc
	}
	return nil
}

// subTestPath returns "/sub_case" for each literal t.Run name whose call
// encloses the line, the way go test names sub-tests (TestX/sub_case).
func subTestPath(fn *ast.FuncDecl, fset *token.FileSet, ln int) string {
	if fn.Body == nil {
		return ""
	}
	var b strings.Builder
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if fset.Position(call.Pos()).Line > ln || fset.Position(call.End()).Line < ln {
			return false
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Run" || len(call.Args) != 2 {
			return true
		}
		lit, ok := call.Args[0].(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		if name, err := strconv.Unquote(lit.Value); err == nil {
			b.WriteString("/" + strings.ReplaceAll(name, " ", "_"))
		}
		return true
	})
	return b.String()
}

// ---- line heuristics for the other languages ----

// symbolizer follows the declarations of a file line by line. Only code lines
// are passed to step; comment and blank lines never are.
type symbolizer interface {
	// enclosing returns the symbol that encloses a comment line, "" for the
	// whole file.
	enclosing(line string) string
	// transparent reports a code line that sits between a doc comment and its
	// declaration without breaking the attachment (a decorator).
	transparent(line string) bool
	// step consumes a code line and returns the full symbol of the
	// declaration it starts, if any.
	step(line string) (string, bool)
	// inLiteral reports that the next line starts inside a multi-line string
	// literal, where nothing is a marker.
	inLiteral() bool
}

func newSymbolizer(l language) symbolizer {
	switch l {
	case langJS:
		return &jsSymbolizer{}
	case langPython:
		return &pySymbolizer{}
	}
	return noSymbolizer{}
}

// noSymbolizer attaches every marker to the whole file.
type noSymbolizer struct{}

func (noSymbolizer) enclosing(string) string    { return "" }
func (noSymbolizer) transparent(string) bool    { return false }
func (noSymbolizer) step(string) (string, bool) { return "", false }
func (noSymbolizer) inLiteral() bool            { return false }

// frame is one open declaration of the heuristic symbolizers.
type frame struct {
	name   string
	test   bool // a describe/it/test title
	class  bool
	call   bool // a test call or a const-bound arrow: may end on its own line
	depth  int  // JS: brace depth before the declaration; Python: indentation
	opened bool // JS: the body brace has been seen
}

// joinFrames renders the symbol of a frame stack: "Type.method" for code,
// "describe > it" between test titles.
func joinFrames(fs []frame) string {
	var b strings.Builder
	for i, f := range fs {
		if i > 0 {
			if f.test || fs[i-1].test {
				b.WriteString(" > ")
			} else {
				b.WriteString(".")
			}
		}
		b.WriteString(f.name)
	}
	return b.String()
}

var (
	jsFuncRE  = regexp.MustCompile(`^(?:export\s+(?:default\s+)?)?(?:async\s+)?function\s*\*?\s*([A-Za-z_$][\w$]*)`)
	jsClassRE = regexp.MustCompile(`^(?:export\s+(?:default\s+)?)?(?:abstract\s+)?class\s+([A-Za-z_$][\w$]*)`)
	jsConstRE = regexp.MustCompile(`^(?:export\s+)?(?:const|let|var)\s+([A-Za-z_$][\w$]*)\s*(?::[^=]*)?=\s*(?:async\s+)?(?:function\b|\(.*=>|[A-Za-z_$][\w$]*\s*=>)`)
	jsTestRE  = regexp.MustCompile(`^(?:describe|it|test|suite|context)(?:\.(?:only|skip|todo|concurrent|each\(.*?\)))?\s*\(\s*(?:'((?:\\.|[^'\\])*)'|"((?:\\.|[^"\\])*)"|` + "`" + `((?:\\.|[^` + "`" + `\\])*)` + "`" + `)`)
	jsMethRE  = regexp.MustCompile(`^(?:(?:public|private|protected|static|async|readonly|override|get|set)\s+)*\*?\s*([A-Za-z_$#][\w$]*)\s*(?:<[^>]*>)?\s*\(`)
)

// jsNotMethods are keywords that look like a method head inside a class body.
var jsNotMethods = map[string]bool{
	"if": true, "for": true, "while": true, "switch": true, "catch": true, "return": true,
	"function": true, "super": true, "await": true, "new": true, "typeof": true,
}

// jsSymbolizer is the TS/JS heuristic: named functions, arrow functions bound
// to a const, classes and their methods, and describe/it/test titles, nested
// by brace depth. Braces inside strings, template literals and comments are
// skipped; regular-expression literals are not recognized.
type jsSymbolizer struct {
	frames   []frame
	depth    int
	inBlock  bool // inside /* */
	template bool // inside a template literal
}

func (j *jsSymbolizer) enclosing(string) string { return joinFrames(j.frames) }

func (j *jsSymbolizer) inLiteral() bool { return j.template }

func (j *jsSymbolizer) transparent(line string) bool {
	return strings.HasPrefix(strings.TrimSpace(line), "@")
}

func (j *jsSymbolizer) step(line string) (string, bool) {
	clean := !j.inBlock && !j.template
	start := j.depth
	closedTo := j.scanBraces(line)
	// A declaration whose body did not open on its own line opens on the next
	// one (a brace on its own line) or never did (a one-line arrow).
	if n := len(j.frames); n > 0 && !j.frames[n-1].opened {
		if j.depth > j.frames[n-1].depth {
			j.frames[n-1].opened = true
		} else {
			j.frames = j.frames[:n-1]
		}
	}
	for n := len(j.frames); n > 0 && closedTo <= j.frames[n-1].depth; n = len(j.frames) {
		j.frames = j.frames[:n-1]
	}
	if !clean {
		return "", false
	}
	s := strings.TrimSpace(line)
	f, ok := j.declaration(s, start)
	if !ok {
		return "", false
	}
	f.depth = start
	f.opened = j.depth > start
	j.frames = append(j.frames, f)
	sym := joinFrames(j.frames)
	if !f.opened && f.call && !continues(s) {
		j.frames = j.frames[:len(j.frames)-1]
	}
	return sym, true
}

// continues reports a call or arrow line whose body follows on the next line.
func continues(s string) bool {
	for _, suf := range []string{"=>", "(", ","} {
		if strings.HasSuffix(s, suf) {
			return true
		}
	}
	return false
}

func (j *jsSymbolizer) declaration(s string, depth int) (frame, bool) {
	if m := jsTestRE.FindStringSubmatch(s); m != nil {
		return frame{name: m[1] + m[2] + m[3], test: true, call: true}, true
	}
	if m := jsClassRE.FindStringSubmatch(s); m != nil {
		return frame{name: m[1], class: true}, true
	}
	if m := jsFuncRE.FindStringSubmatch(s); m != nil {
		return frame{name: m[1]}, true
	}
	if m := jsConstRE.FindStringSubmatch(s); m != nil {
		return frame{name: m[1], call: true}, true
	}
	if n := len(j.frames); n > 0 && j.frames[n-1].class && j.frames[n-1].opened && depth == j.frames[n-1].depth+1 {
		if m := jsMethRE.FindStringSubmatch(s); len(m) > 1 && !jsNotMethods[m[1]] && strings.Contains(s, "{") {
			return frame{name: m[1]}, true
		}
	}
	return frame{}, false
}

// scanBraces updates the brace depth over one line and returns the lowest
// depth a closing brace brought it to, math.MaxInt when none closed.
func (j *jsSymbolizer) scanBraces(line string) int {
	minDepth := math.MaxInt
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case j.inBlock:
			if c == '*' && i+1 < len(line) && line[i+1] == '/' {
				j.inBlock = false
				i++
			}
		case j.template:
			switch c {
			case '\\':
				i++
			case '`':
				j.template = false
			}
		case c == '/' && i+1 < len(line) && line[i+1] == '/':
			return minDepth
		case c == '/' && i+1 < len(line) && line[i+1] == '*':
			j.inBlock = true
			i++
		case c == '"' || c == '\'':
			i = skipQuoted(line, i) - 1
		case c == '`':
			j.template = true
		case c == '{':
			j.depth++
		case c == '}':
			j.depth--
			if j.depth < minDepth {
				minDepth = j.depth
			}
		}
	}
	return minDepth
}

var (
	pyDefRE   = regexp.MustCompile(`^(?:async\s+)?def\s+([A-Za-z_]\w*)`)
	pyClassRE = regexp.MustCompile(`^class\s+([A-Za-z_]\w*)`)
)

// pySymbolizer is the Python heuristic: def and class, nested by indentation.
type pySymbolizer struct {
	frames []frame
}

func indentOf(line string) int {
	n := 0
	for _, c := range line {
		switch c {
		case ' ':
			n++
		case '\t':
			n += 8 - n%8
		default:
			return n
		}
	}
	return n
}

// open returns the frames that enclose a line at the given indentation.
func (p *pySymbolizer) open(indent int) []frame {
	n := len(p.frames)
	for n > 0 && p.frames[n-1].depth >= indent {
		n--
	}
	return p.frames[:n]
}

func (p *pySymbolizer) enclosing(line string) string {
	return joinFrames(p.open(indentOf(line)))
}

func (p *pySymbolizer) inLiteral() bool { return false }

func (p *pySymbolizer) transparent(line string) bool {
	return strings.HasPrefix(strings.TrimSpace(line), "@")
}

func (p *pySymbolizer) step(line string) (string, bool) {
	indent := indentOf(line)
	p.frames = p.open(indent)
	s := strings.TrimSpace(line)
	var name string
	if m := pyDefRE.FindStringSubmatch(s); m != nil {
		name = m[1]
	} else if m := pyClassRE.FindStringSubmatch(s); m != nil {
		name = m[1]
	} else {
		return "", false
	}
	p.frames = append(p.frames, frame{name: name, depth: indent})
	return joinFrames(p.frames), true
}
