package server

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// sourceExts are the languages Pando builds call edges for that the sampler
// reads.
var sourceExts = map[string]bool{".go": true, ".ts": true, ".tsx": true, ".js": true, ".py": true, ".rs": true, ".java": true}

const (
	// sampleMaxFileBytes skips files too large to be worth parsing for a hint.
	sampleMaxFileBytes = 256 << 10
	// sampleMaxFiles bounds the walk on a huge tree.
	sampleMaxFiles = 5000
)

// sampleCandidate is one source file and its coupling score.
type sampleCandidate struct {
	rel   string
	dir   string
	score int
}

// sampleSourceFiles picks up to n source files of a tree that are likely to
// have call edges, so a graph check that asks Pando about them stops at the
// first coupled answer (GIT-US-0190). Pando's related-files answer needs a call
// edge or an import into or out of the file, and a file of a main package
// mostly has neither in the direction that counts, so the ranking prefers
// library packages, files that declare many exported functions (other files
// call them) and files that call into other packages. It spreads the sample:
// the best file of each directory first, best directory first, then the
// runners-up. Deterministic for a given tree; the tree is only read.
func sampleSourceFiles(root string, n int) []string {
	if n <= 0 {
		return nil
	}
	var cands []sampleCandidate
	seen := 0
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // an unreadable entry is skipped, the sample is best effort
		}
		name := d.Name()
		if d.IsDir() {
			if p != root && (strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor" || name == "testdata" || name == "dist") {
				return filepath.SkipDir
			}
			return nil
		}
		if !sourceExts[filepath.Ext(name)] || generatedOrTest(name) {
			return nil
		}
		seen++
		if seen > sampleMaxFiles {
			return fs.SkipAll
		}
		if rel, e := filepath.Rel(root, p); e == nil {
			rel = filepath.ToSlash(rel)
			cands = append(cands, sampleCandidate{rel: rel, dir: path.Dir(rel), score: coupling(p)})
		}
		return nil
	})
	return spreadSample(cands, n)
}

// generatedOrTest reports a file name that is a test or generated code.
func generatedOrTest(name string) bool {
	for _, suffix := range []string{"_test.go", ".pb.go", "_gen.go", ".gen.go", ".d.ts", ".test.ts", ".test.tsx", ".spec.ts", ".spec.tsx", "_test.py"} {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return strings.HasPrefix(name, "test_") && strings.HasSuffix(name, ".py")
}

// spreadSample orders candidates best per directory first, then the rest, and
// returns the first n. A candidate scoring 0 is used only to fill.
func spreadSample(cands []sampleCandidate, n int) []string {
	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].score != cands[j].score {
			return cands[i].score > cands[j].score
		}
		return cands[i].rel < cands[j].rel
	})
	var out []string
	taken := map[string]bool{}
	usedDir := map[string]bool{}
	for _, c := range cands {
		if len(out) == n {
			return out
		}
		if !usedDir[c.dir] {
			usedDir[c.dir] = true
			taken[c.rel] = true
			out = append(out, c.rel)
		}
	}
	for _, c := range cands {
		if len(out) == n {
			break
		}
		if !taken[c.rel] {
			out = append(out, c.rel)
		}
	}
	return out
}

// coupling scores how likely a file is to have call or import edges. Go files
// are read with the parser; other languages by counting declaration keywords.
// It is a hint, so an unreadable or oversized file scores 0.
func coupling(p string) int {
	info, err := os.Stat(p)
	if err != nil || info.Size() > sampleMaxFileBytes {
		return 0
	}
	src, err := os.ReadFile(p)
	if err != nil {
		return 0
	}
	if filepath.Ext(p) == ".go" {
		return goCoupling(p, src)
	}
	return keywordCoupling(filepath.Ext(p), src)
}

// goCoupling ranks a Go file by its exported top-level functions and methods
// (callers elsewhere resolve to them) plus its calls through another package
// (`pkg.Func(...)`). A main package's score is quartered: it is called by no
// one, and its outgoing edges are the only coupling it can offer.
func goCoupling(file string, src []byte) int {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, src, parser.SkipObjectResolution)
	if err != nil {
		return 0
	}
	exported, calls := 0, 0
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		if fn.Name.IsExported() {
			exported++
		}
		ast.Inspect(fn.Body, func(nd ast.Node) bool {
			if call, ok := nd.(*ast.CallExpr); ok {
				if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
					if _, ok := sel.X.(*ast.Ident); ok {
						calls++
					}
				}
			}
			return true
		})
	}
	// Exported functions weigh more than calls: a file that others call and
	// that calls others is coupled in both directions.
	score := exported*4 + min(calls, 40)
	if exported > 0 && calls > 0 {
		score += 10
	}
	if f.Name.Name == "main" {
		score /= 4
	}
	return score
}

// keywordCoupling counts the declarations other files can import or call.
func keywordCoupling(ext string, src []byte) int {
	var kw string
	switch ext {
	case ".ts", ".tsx", ".js":
		kw = "export "
	case ".py":
		kw = "\ndef "
	case ".rs":
		kw = "pub fn "
	case ".java":
		kw = "public "
	}
	if kw == "" {
		return 0
	}
	return min(bytes.Count(src, []byte(kw)), 40)
}
