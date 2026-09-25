package trace

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path"
	"sort"
	"strings"
)

// Package-level declarations reach traced code (GIT-US-0158, docs/03 R-IMP-2).
// A diff that changes a top-level const, var or type of a Go file changes the
// behavior of every function that uses it, yet no traced symbol spans the
// declaration itself: the spec impact benchmark missed a change of the page
// cap `maxPageSize` that way. The reverse query therefore maps such a change
// to the functions of the same package that reference the name, with the
// scope rules of go/parser's syntax tree — a local declaration of the same
// name shadows it, a selector's field or method never matches — so the answer
// is deterministic and needs no Pando and no type checking. It is one hop: a
// package-level var whose initializer uses the changed name is not followed.

// declRef is one function that references a changed package-level name.
type declRef struct {
	path   string // the file of the function
	symbol string // the function, in the trace-ref spelling (Func, Type.Method)
	name   string // the changed name it references
}

// changedDecls returns the package clause of a Go file and the names of the
// package-level const, var and type specs whose symbol — the first name of the
// spec, as the scanner spells it — is among the changed symbols. A file that
// does not parse yields nothing.
func changedDecls(p string, src []byte, symbols []string) (pkg string, names []string) {
	if len(symbols) == 0 {
		return "", nil
	}
	changed := map[string]bool{}
	for _, s := range symbols {
		if s != "" {
			changed[s] = true
		}
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, p, src, parser.SkipObjectResolution)
	if f == nil || err != nil {
		return "", nil
	}
	seen := map[string]bool{}
	for _, d := range f.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok == token.IMPORT {
			continue
		}
		for _, s := range gd.Specs {
			if !changed[specName(s)] {
				continue
			}
			for _, id := range specNames(s) {
				if id.Name != "_" && !seen[id.Name] {
					seen[id.Name] = true
					names = append(names, id.Name)
				}
			}
		}
	}
	sort.Strings(names)
	return f.Name.Name, names
}

// specNames returns every name a const, var or type spec declares.
func specNames(s ast.Spec) []*ast.Ident {
	switch s := s.(type) {
	case *ast.TypeSpec:
		return []*ast.Ident{s.Name}
	case *ast.ValueSpec:
		return s.Names
	}
	return nil
}

// declReferences returns the functions of the Go package of p — the .go files
// of p's directory whose package clause is pkg — that reference one of names
// as the package-level declaration, sorted by path, symbol and name. Only the
// files traced accepts are read, so a package without trace edges costs one
// directory listing.
func declReferences(tree fs.FS, p, pkg string, names []string, traced func(string) bool) []declRef {
	if tree == nil || pkg == "" || len(names) == 0 {
		return nil
	}
	want := map[string]bool{}
	for _, n := range names {
		want[n] = true
	}
	dir := path.Dir(p)
	entries, err := fs.ReadDir(tree, dir)
	if err != nil {
		return nil
	}
	var out []declRef
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		q := path.Join(dir, entry.Name())
		if !traced(q) {
			continue
		}
		src, err := fs.ReadFile(tree, q)
		if err != nil {
			continue
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, q, src, parser.SkipObjectResolution)
		if f == nil || err != nil || f.Name.Name != pkg {
			continue
		}
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok {
				continue
			}
			w := &refWalker{names: want, shadow: map[string]int{}, found: map[string]bool{}}
			w.funcDecl(fn)
			symbol := funcName(fn)
			for n := range w.found {
				out = append(out, declRef{path: q, symbol: symbol, name: n})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.path != b.path {
			return a.path < b.path
		}
		if a.symbol != b.symbol {
			return a.symbol < b.symbol
		}
		return a.name < b.name
	})
	return out
}

// refWalker finds the uses of a set of package-level names in one function,
// honoring the block scopes of the syntax tree: a name declared locally — a
// parameter, a := or a var, const or type statement, a range or type-switch
// variable — shadows the package-level one inside its scope. Struct field
// names, composite-literal keys of a struct and the selected name of a
// selector are never uses; labels live in their own namespace.
type refWalker struct {
	names  map[string]bool
	shadow map[string]int
	scopes [][]string
	found  map[string]bool
}

func (w *refWalker) push() { w.scopes = append(w.scopes, nil) }

func (w *refWalker) pop() {
	top := w.scopes[len(w.scopes)-1]
	for _, n := range top {
		w.shadow[n]--
	}
	w.scopes = w.scopes[:len(w.scopes)-1]
}

// declare shadows the watched names among ids in the innermost scope.
func (w *refWalker) declare(ids ...*ast.Ident) {
	for _, id := range ids {
		if id == nil || !w.names[id.Name] {
			continue
		}
		w.shadow[id.Name]++
		w.scopes[len(w.scopes)-1] = append(w.scopes[len(w.scopes)-1], id.Name)
	}
}

// fields walks the types of a field list, then declares its names.
func (w *refWalker) fields(fl *ast.FieldList) {
	if fl == nil {
		return
	}
	for _, f := range fl.List {
		w.walk(f.Type)
	}
	for _, f := range fl.List {
		w.declare(f.Names...)
	}
}

// funcDecl walks a top-level function: receiver, type parameters,
// parameters and results are one scope around the body.
func (w *refWalker) funcDecl(fn *ast.FuncDecl) {
	w.push()
	w.fields(fn.Recv)
	w.funcType(fn.Type)
	if fn.Body != nil {
		w.walk(fn.Body)
	}
	w.pop()
}

func (w *refWalker) funcType(ft *ast.FuncType) {
	w.fields(ft.TypeParams)
	w.fields(ft.Params)
	w.fields(ft.Results)
}

// walkAll walks the immediate children of n.
func (w *refWalker) walkAll(n ast.Node) {
	ast.Inspect(n, func(c ast.Node) bool {
		if c == n {
			return true
		}
		if c != nil {
			w.walk(c)
		}
		return false
	})
}

// walk visits one node. It is a hand-rolled traversal because the scope of a
// name depends on the statement that declares it.
func (w *refWalker) walk(n ast.Node) {
	switch n := n.(type) {
	case nil:
	case *ast.Ident:
		if w.names[n.Name] && w.shadow[n.Name] == 0 {
			w.found[n.Name] = true
		}
	case *ast.SelectorExpr:
		w.walk(n.X)
	case *ast.CompositeLit:
		w.walk(n.Type)
		_, isMap := n.Type.(*ast.MapType)
		_, isArray := n.Type.(*ast.ArrayType)
		for _, e := range n.Elts {
			kv, ok := e.(*ast.KeyValueExpr)
			if !ok {
				w.walk(e)
				continue
			}
			// A bare key of a struct literal (or of an elided type) is a field
			// name; a map or array key is an expression.
			if _, bare := kv.Key.(*ast.Ident); !bare || isMap || isArray {
				w.walk(kv.Key)
			}
			w.walk(kv.Value)
		}
	case *ast.Field:
		w.walk(n.Type)
	case *ast.FuncLit:
		w.push()
		w.funcType(n.Type)
		w.walk(n.Body)
		w.pop()
	case *ast.BlockStmt:
		w.push()
		for _, s := range n.List {
			w.walk(s)
		}
		w.pop()
	case *ast.AssignStmt:
		for _, e := range n.Rhs {
			w.walk(e)
		}
		for _, e := range n.Lhs {
			if id, ok := e.(*ast.Ident); ok && n.Tok == token.DEFINE {
				w.declare(id)
				continue
			}
			w.walk(e)
		}
	case *ast.GenDecl:
		for _, s := range n.Specs {
			switch s := s.(type) {
			case *ast.ValueSpec:
				w.walk(s.Type)
				for _, v := range s.Values {
					w.walk(v)
				}
				w.declare(s.Names...)
			case *ast.TypeSpec:
				w.declare(s.Name)
				w.fields(s.TypeParams)
				w.walk(s.Type)
			}
		}
	case *ast.RangeStmt:
		w.push()
		w.walk(n.X)
		for _, e := range []ast.Expr{n.Key, n.Value} {
			if id, ok := e.(*ast.Ident); ok && n.Tok == token.DEFINE {
				w.declare(id)
				continue
			}
			w.walk(e)
		}
		w.walk(n.Body)
		w.pop()
	case *ast.ForStmt:
		w.push()
		w.walk(n.Init)
		w.walk(n.Cond)
		w.walk(n.Post)
		w.walk(n.Body)
		w.pop()
	case *ast.IfStmt:
		w.push()
		w.walk(n.Init)
		w.walk(n.Cond)
		w.walk(n.Body)
		w.walk(n.Else)
		w.pop()
	case *ast.SwitchStmt:
		w.push()
		w.walk(n.Init)
		w.walk(n.Tag)
		w.walk(n.Body)
		w.pop()
	case *ast.TypeSwitchStmt:
		w.push()
		w.walk(n.Init)
		w.walk(n.Assign)
		w.walk(n.Body)
		w.pop()
	case *ast.CaseClause:
		w.push()
		for _, e := range n.List {
			w.walk(e)
		}
		for _, s := range n.Body {
			w.walk(s)
		}
		w.pop()
	case *ast.CommClause:
		w.push()
		w.walk(n.Comm)
		for _, s := range n.Body {
			w.walk(s)
		}
		w.pop()
	case *ast.LabeledStmt:
		w.walk(n.Stmt)
	case *ast.BranchStmt:
	default:
		w.walkAll(n)
	}
}
