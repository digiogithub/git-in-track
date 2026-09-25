package trace

import (
	"bufio"
	"bytes"
	"io/fs"
	"path"
	"sort"
	"strings"
)

// TestResolver maps the tests a report names to files of one working tree, so
// that a result carries the same "<path>#<symbol>" a Verifies: marker or a
// trace.tests entry spells (docs/07 section 4.19 documents the rules).
//
//   - go test: the package import path is mapped to a directory — through the
//     module path of the root go.mod, else through the longest suffix of the
//     import path that is a directory of the tree — and the test to the
//     _test.go file of that directory that declares the top-level TestX.
//     The symbol is the test name as go test prints it (TestX/sub_case).
//   - Vitest: the test file the report names; the symbol is the ancestor
//     titles and the title joined with " > ".
//   - JUnit: the file attribute when there is one; else the classname read
//     as a Go import path, then as a file path, then as a dotted Python
//     module (tests.test_mod.TestCase -> tests/test_mod.py#TestCase.name).
//     The symbol is the testcase name, qualified by the classname's class
//     part for a Python file.
//
// A report path is used as is when it is relative and exists; an absolute
// path under the root is made relative; any other path — an absolute path
// from another checkout, the CI runner's — is mapped to its longest suffix
// that exists in the tree. When two _test.go files of a package declare the
// same test (build-tagged variants), the first in lexical order wins and the
// others are listed in TestResult.Ambiguous. A test that maps to no file
// keeps its report-level id and an empty Path; it never matches a trace ref.
type TestResolver struct {
	tree   fs.FS
	root   string
	base   string
	module string
	dirs   map[string]map[string][]string // dir -> top-level test -> files
}

// NewTestResolver returns a resolver over the working tree tree, whose absolute
// location is root ("" when unknown: absolute report paths are then mapped
// by suffix only). base is the repository-relative directory the report's
// relative paths are relative to ("" or "." for the root), e.g. "web" for a
// Vitest run started in web/.
func NewTestResolver(tree fs.FS, root, base string) *TestResolver {
	r := &TestResolver{
		tree: tree, root: strings.TrimSuffix(toSlash(root), "/"), base: cleanRel(base),
		dirs: map[string]map[string][]string{},
	}
	if data, err := fs.ReadFile(tree, "go.mod"); err == nil {
		r.module = modulePath(data)
	}
	return r
}

// Resolve maps one raw result to a TestResult. Commit and At are left for
// the caller to set.
func (r *TestResolver) Resolve(raw RawResult) TestResult {
	out := TestResult{
		ID: raw.ID(), Format: raw.Format, Symbol: raw.Name, Result: raw.Outcome, Duration: raw.Duration,
	}
	switch raw.Format {
	case FormatGoTest:
		out.Path, out.Ambiguous = r.goFile(raw.Scope, raw.Name)
	case FormatVitest:
		out.Path = r.file(raw.File)
	case FormatJUnit:
		r.resolveJUnit(raw, &out)
	}
	return out
}

func (r *TestResolver) resolveJUnit(raw RawResult, out *TestResult) {
	if raw.File != "" {
		if p := r.file(raw.File); p != "" {
			out.Path = p
			out.Symbol = pythonSymbol(p, raw.Scope, raw.Name)
			return
		}
	}
	if raw.Scope == "" {
		return
	}
	if p, amb := r.goFile(raw.Scope, raw.Name); p != "" {
		out.Path, out.Ambiguous = p, amb
		return
	}
	if p := r.file(raw.Scope); p != "" {
		out.Path = p
		return
	}
	segs := strings.Split(raw.Scope, ".")
	for i := len(segs); i >= 1; i-- {
		p := r.file(strings.Join(segs[:i], "/") + ".py")
		if p == "" {
			continue
		}
		out.Path = p
		out.Symbol = strings.Join(append(append([]string{}, segs[i:]...), raw.Name), ".")
		return
	}
}

// pythonSymbol qualifies a JUnit name with the class part of its classname
// when the file is Python: tests/test_mod.py with classname
// tests.test_mod.TestCase and name test_x is TestCase.test_x.
func pythonSymbol(p, classname, name string) string {
	if !strings.HasSuffix(p, ".py") || classname == "" {
		return name
	}
	mod := strings.ReplaceAll(strings.TrimSuffix(p, ".py"), "/", ".")
	for m := mod; m != ""; {
		if rest, ok := strings.CutPrefix(classname, m+"."); ok && rest != "" {
			return rest + "." + name
		}
		_, m, _ = strings.Cut(m, ".")
	}
	return name
}

// file maps a path a report names to a repository-relative file of the
// tree, "" when none.
func (r *TestResolver) file(raw string) string {
	p := toSlash(strings.TrimSpace(raw))
	p = strings.TrimPrefix(p, "file://")
	if p == "" {
		return ""
	}
	if !isAbs(p) {
		if strings.Contains(p, "/../") || strings.HasPrefix(p, "../") {
			return r.suffix(p)
		}
		for _, cand := range []string{path.Join(r.base, p), p} {
			if c := cleanRel(cand); c != "" && r.isFile(c) {
				return c
			}
		}
		return r.suffix(p)
	}
	if r.root != "" {
		if rest, ok := strings.CutPrefix(p, r.root+"/"); ok {
			if c := cleanRel(rest); c != "" && r.isFile(c) {
				return c
			}
		}
	}
	return r.suffix(p)
}

// suffix returns the longest proper suffix of p that is a file of the tree.
func (r *TestResolver) suffix(p string) string {
	segs := strings.Split(strings.Trim(path.Clean("/"+p), "/"), "/")
	for i := 1; i < len(segs); i++ {
		c := strings.Join(segs[i:], "/")
		if r.isFile(c) {
			return c
		}
	}
	return ""
}

func (r *TestResolver) isFile(p string) bool {
	if r.tree == nil || !fs.ValidPath(p) {
		return false
	}
	st, err := fs.Stat(r.tree, p)
	return err == nil && st.Mode().IsRegular()
}

func (r *TestResolver) isDir(p string) bool {
	if r.tree == nil || !fs.ValidPath(p) {
		return false
	}
	st, err := fs.Stat(r.tree, p)
	return err == nil && st.IsDir()
}

// goFile maps a Go package import path and a test name to the _test.go file
// that declares the test, with the other candidates when there are several.
func (r *TestResolver) goFile(importPath, test string) (file string, ambiguous []string) {
	importPath = strings.TrimSpace(importPath)
	top, _, _ := strings.Cut(test, "/")
	if importPath == "" || top == "" {
		return "", nil
	}
	var dirs []string
	if r.module != "" {
		if importPath == r.module {
			dirs = append(dirs, ".")
		} else if rest, ok := strings.CutPrefix(importPath, r.module+"/"); ok {
			dirs = append(dirs, rest)
		}
	}
	segs := strings.Split(importPath, "/")
	for i := 1; i < len(segs); i++ {
		dirs = append(dirs, strings.Join(segs[i:], "/"))
	}
	for _, d := range dirs {
		if !fs.ValidPath(d) || !r.isDir(d) {
			continue
		}
		if files := r.dirTests(d)[top]; len(files) > 0 {
			var amb []string
			if len(files) > 1 {
				amb = append(amb, files[1:]...)
			}
			return files[0], amb
		}
	}
	return "", nil
}

// dirTests lists the top-level functions each _test.go file of a directory
// declares, parsed once per directory.
func (r *TestResolver) dirTests(dir string) map[string][]string {
	if m, ok := r.dirs[dir]; ok {
		return m
	}
	m := map[string][]string{}
	r.dirs[dir] = m
	entries, err := fs.ReadDir(r.tree, dir)
	if err != nil {
		return m
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		p := path.Join(dir, e.Name())
		src, err := fs.ReadFile(r.tree, p)
		if err != nil {
			continue
		}
		syms, ok := DeclaredSymbols(p, src)
		if !ok {
			continue
		}
		for _, s := range syms {
			if !strings.ContainsAny(s, "./") {
				m[s] = append(m[s], p)
			}
		}
	}
	for _, files := range m {
		sort.Strings(files)
	}
	return m
}

// modulePath reads the module directive of a go.mod file.
func modulePath(data []byte) string {
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if rest, ok := strings.CutPrefix(line, "module"); ok && (rest == "" || rest[0] == ' ' || rest[0] == '\t') {
			rest = strings.TrimSpace(rest)
			if i := strings.Index(rest, "//"); i >= 0 {
				rest = strings.TrimSpace(rest[:i])
			}
			return strings.Trim(rest, `"`+"`")
		}
	}
	return ""
}

func toSlash(p string) string { return strings.ReplaceAll(p, "\\", "/") }

// isAbs reports a "/"-separated absolute path, a Windows drive path
// included.
func isAbs(p string) bool {
	if strings.HasPrefix(p, "/") {
		return true
	}
	return len(p) >= 3 && p[1] == ':' && p[2] == '/'
}

// cleanRel cleans a relative path; "" for the root or a path that escapes
// it.
func cleanRel(p string) string {
	p = path.Clean(toSlash(p))
	if p == "." || p == "" || p == ".." || strings.HasPrefix(p, "../") || strings.HasPrefix(p, "/") {
		return ""
	}
	return p
}
