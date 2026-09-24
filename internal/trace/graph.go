package trace

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"

	"github.com/digiogithub/git-in-track/internal/core"
)

// Graph is the requirement trace graph (ADR-037 sections 4, 5 and 8; docs/03
// section 21.7): for every requirement of the index, the code and tests that
// markers and trace: entries tie to it, and the stories and tasks that
// implement or modify it; and, the other way round, for every traced path the
// requirements that reach it. It is derived data, rebuilt from the index, the
// marker scan and the working tree, and never written anywhere. A Graph is
// immutable once built and safe for concurrent reads.
//
// Every list it returns is sorted, so the same repository state always yields
// the same answer.
type Graph struct {
	reqs   map[core.RequirementRef]*core.TracedRequirement
	order  []core.RequirementRef
	byPath map[string][]core.TraceEdge
	broken []core.TraceBroken
}

// edgeKey identifies an edge; duplicates of it collapse (R-MARK-3).
type edgeKey struct {
	ref    core.RequirementRef
	role   core.TraceRole
	path   string
	symbol string
}

// BuildGraph builds the trace graph of one repository. ix is its index, the
// source of the requirements, their trace: entries and the implements /
// modifies links; markers is the marker scan (Cache.Markers); tree reads the
// working tree, rooted at the repository root, to check that each trace:
// entry still resolves. A nil tree skips that check.
//
// A marker whose requirement is not in the index is left out: it is a
// dangling marker, which Dangling reports.
func BuildGraph(ix *core.Index, markers []Marker, tree fs.FS) (*Graph, error) {
	views, err := ix.Requirements(core.RequirementFilter{})
	if err != nil {
		return nil, fmt.Errorf("trace graph: %w", err)
	}
	g := &Graph{reqs: map[core.RequirementRef]*core.TracedRequirement{}, byPath: map[string][]core.TraceEdge{}}
	edges := map[edgeKey]*core.TraceEdge{}
	add := func(k edgeKey, src core.TraceSource, line int) {
		e, ok := edges[k]
		if !ok {
			e = &core.TraceEdge{Ref: k.ref, Role: k.role, Path: k.path, Symbol: k.symbol}
			edges[k] = e
		}
		if !containsSource(e.Sources, src) {
			e.Sources = append(e.Sources, src)
		}
		if line > 0 {
			e.Lines = append(e.Lines, line)
		}
	}
	files := newTreeReader(tree)
	for _, v := range views {
		tr := &core.TracedRequirement{Ref: v.Ref, Project: v.Project}
		g.reqs[v.Ref] = tr
		g.order = append(g.order, v.Ref)
		tr.Work = workOf(ix, v.Ref)
		if v.Trace == nil {
			continue
		}
		for _, entry := range []struct {
			field string
			role  core.TraceRole
			refs  []string
		}{
			{"trace.code", core.TraceRoleCode, v.Trace.Code},
			{"trace.tests", core.TraceRoleTest, v.Trace.Tests},
		} {
			for _, raw := range entry.refs {
				p, symbol, ok := splitTraceRef(raw)
				if !ok {
					continue // not a trace ref at all: the validator reports it
				}
				add(edgeKey{v.Ref, entry.role, p, symbol}, core.TraceSourceEntry, 0)
				if why := files.check(p, symbol); why != "" {
					tr.Broken = append(tr.Broken, core.TraceBroken{
						Ref: v.Ref, Field: entry.field, Entry: raw,
						Code: core.CodeWarnTraceBroken, Severity: core.SeverityWarning,
						Message: fmt.Sprintf("%s of %s: %s", entry.field, v.Ref, why),
					})
				}
			}
		}
	}
	for _, m := range markers {
		if _, ok := g.reqs[m.Ref]; !ok {
			continue
		}
		role := core.TraceRoleCode
		if m.Kind == KindVerifies {
			role = core.TraceRoleTest
		}
		add(edgeKey{m.Ref, role, m.Path, m.Symbol}, core.TraceSourceMarker, m.Line)
	}
	for _, e := range edges {
		sortEdge(e)
		tr := g.reqs[e.Ref]
		if e.Role == core.TraceRoleCode {
			tr.Code = append(tr.Code, *e)
		} else {
			tr.Tests = append(tr.Tests, *e)
		}
		g.byPath[e.Path] = append(g.byPath[e.Path], *e)
	}
	sortRefs(g.order)
	for _, ref := range g.order {
		tr := g.reqs[ref]
		sortEdges(tr.Code)
		sortEdges(tr.Tests)
		sortBroken(tr.Broken)
		if tr.Code == nil {
			tr.Code = []core.TraceEdge{}
		}
		if tr.Tests == nil {
			tr.Tests = []core.TraceEdge{}
		}
		g.broken = append(g.broken, tr.Broken...)
	}
	for p := range g.byPath {
		sortEdges(g.byPath[p])
	}
	return g, nil
}

// workOf lists the live stories and tasks that implement or modify a
// requirement, directly or through its whole spec. A direct link wins over a
// whole-spec link of the same item and kind.
func workOf(ix *core.Index, ref core.RequirementRef) []core.TraceWork {
	type key struct {
		id   core.ItemID
		kind core.LinkKind
	}
	seen := map[key]int{}
	out := []core.TraceWork{}
	collect := func(target string, whole bool) {
		for _, pair := range []struct{ inverse, kind core.LinkKind }{
			{core.LinkImplementedBy, core.LinkImplements},
			{core.LinkModifiedBy, core.LinkModifies},
		} {
			for _, id := range ix.Related(target, pair.inverse) {
				it, err := ix.Item(id)
				if err != nil || it.Deleted {
					continue
				}
				k := key{id, pair.kind}
				if i, ok := seen[k]; ok {
					if !whole {
						out[i].WholeSpec = false
					}
					continue
				}
				seen[k] = len(out)
				out = append(out, core.TraceWork{ID: id, Kind: pair.kind, WholeSpec: whole})
			}
		}
	}
	collect(ref.String(), false)
	collect(string(ref.Spec), true)
	sort.Slice(out, func(i, j int) bool {
		if out[i].ID != out[j].ID {
			return out[i].ID < out[j].ID
		}
		return out[i].Kind < out[j].Kind
	})
	return out
}

// Requirement returns the trace of one requirement; false when the index has
// no such requirement.
func (g *Graph) Requirement(ref core.RequirementRef) (core.TracedRequirement, bool) {
	tr, ok := g.reqs[ref]
	if !ok {
		return core.TracedRequirement{}, false
	}
	return cloneTraced(*tr), true
}

// Requirements returns the trace of every requirement, sorted by spec and
// number.
func (g *Graph) Requirements() []core.TracedRequirement {
	out := make([]core.TracedRequirement, 0, len(g.order))
	for _, ref := range g.order {
		out = append(out, cloneTraced(*g.reqs[ref]))
	}
	return out
}

// Paths returns every path some edge reaches, sorted.
func (g *Graph) Paths() []string {
	out := make([]string, 0, len(g.byPath))
	for p := range g.byPath {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// ForPath returns every edge into one path, whole-file and symbol edges alike,
// sorted: the requirements a file realizes or verifies.
func (g *Graph) ForPath(p string) []core.TraceEdge {
	return cloneEdges(g.byPath[cleanPath(p)])
}

// ForSymbol returns the edges a symbol of a path reaches: the whole-file
// edges of the path and every symbol edge whose symbol is the same as, or
// encloses or is enclosed by, the given one. An empty symbol returns only the
// whole-file edges.
func (g *Graph) ForSymbol(p, symbol string) []core.TraceEdge {
	var out []core.TraceEdge
	for _, e := range g.byPath[cleanPath(p)] {
		if e.Symbol == "" || (symbol != "" && symbolsOverlap(e.Symbol, symbol)) {
			out = append(out, e)
		}
	}
	return cloneEdges(out)
}

// Broken returns the W-TRACE-BROKEN findings of every requirement, sorted by
// requirement, field and entry.
func (g *Graph) Broken() []core.TraceBroken {
	return append([]core.TraceBroken(nil), g.broken...)
}

// Touching answers the reverse query of impact tier 1: the edges a set of
// changes touches. For each change, tree (the working tree at the new side)
// maps the changed lines to the symbols that enclose them with the scanner's
// own spans; Change.Symbols replaces that mapping when set. A change with no
// lines, or whose file cannot be read on the new side (it was deleted),
// touches every edge of its path; the old path of a rename touches every edge
// of the old path. Hits are deduplicated and sorted by path, symbol, ref and
// role.
func (g *Graph) Touching(changes []core.TraceChange, tree fs.FS) []core.TraceHit {
	return g.TouchingSince(nil, changes, tree)
}

// TouchingSince is Touching with the graph as it was before the changes were
// scanned: an edge of prev into a changed path that g no longer holds — a
// marker deleted with its line or its file — is a hit too, with the reason
// "removed". A nil prev is Touching.
func (g *Graph) TouchingSince(prev *Graph, changes []core.TraceChange, tree fs.FS) []core.TraceHit {
	seen := map[edgeKey]bool{}
	var out []core.TraceHit
	hit := func(e core.TraceEdge, reason, changed string) {
		k := edgeKey{e.Ref, e.Role, e.Path, e.Symbol}
		if seen[k] {
			return
		}
		seen[k] = true
		out = append(out, core.TraceHit{TraceEdge: cloneEdges([]core.TraceEdge{e})[0], Reason: reason, Changed: changed})
	}
	for _, c := range changes {
		p := cleanPath(c.Path)
		if old := cleanPath(c.OldPath); c.OldPath != "" && old != p {
			for _, e := range g.byPath[old] {
				hit(e, "renamed", "")
			}
		}
		if prev != nil {
			for _, q := range []string{p, cleanPath(c.OldPath)} {
				for _, e := range prev.byPath[q] {
					if q != "" && !g.hasEdge(e) {
						hit(e, "removed", "")
					}
				}
			}
		}
		edges := g.byPath[p]
		if len(edges) == 0 {
			continue
		}
		symbols, lines, whole := changedSymbols(c, p, tree)
		for _, e := range edges {
			switch {
			case whole || e.Symbol == "":
				hit(e, "file", "")
			case markerLineChanged(e, lines):
				hit(e, "marker", "")
			default:
				for _, s := range symbols {
					if s != "" && symbolsOverlap(e.Symbol, s) {
						hit(e, "symbol", s)
						break
					}
				}
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		if a.Symbol != b.Symbol {
			return a.Symbol < b.Symbol
		}
		if c := compareRefs(a.Ref, b.Ref); c != 0 {
			return c < 0
		}
		return a.Role < b.Role
	})
	return out
}

// hasEdge reports whether the graph holds an edge with the same ref, role,
// path and symbol.
func (g *Graph) hasEdge(e core.TraceEdge) bool {
	for _, x := range g.byPath[e.Path] {
		if x.Ref == e.Ref && x.Role == e.Role && x.Symbol == e.Symbol {
			return true
		}
	}
	return false
}

// changedSymbols returns the symbols a change touches, its changed lines,
// and whether it must be treated as a whole-file change.
func changedSymbols(c core.TraceChange, p string, tree fs.FS) (symbols []string, lines map[int]bool, whole bool) {
	if len(c.Symbols) > 0 {
		return c.Symbols, nil, false
	}
	if len(c.Lines) == 0 || tree == nil {
		return nil, nil, true
	}
	src, err := fs.ReadFile(tree, p)
	if err != nil {
		return nil, nil, true
	}
	lines = map[int]bool{}
	var list []int
	for _, span := range c.Lines {
		start, n := span.Start, span.Count
		if start < 1 {
			start = 1
		}
		if n < 1 {
			n = 1 // a pure deletion touches the line it happened before
		}
		for ln := start; ln < start+n; ln++ {
			if !lines[ln] {
				lines[ln] = true
				list = append(list, ln)
			}
		}
	}
	at := SymbolsAt(p, src, list)
	set := map[string]bool{}

	for _, ln := range list {
		if s := at[ln]; !set[s] {
			set[s] = true
			symbols = append(symbols, s)
		}
	}
	sort.Strings(symbols)
	return symbols, lines, false
}

func markerLineChanged(e core.TraceEdge, lines map[int]bool) bool {
	for _, ln := range e.Lines {
		if lines[ln] {
			return true
		}
	}
	return false
}

// splitTraceRef splits "<path>[#<symbol>]", refusing what the trace-ref
// grammar of R-REQ-8 refuses.
func splitTraceRef(raw string) (p, symbol string, ok bool) {
	p, symbol, hasSymbol := strings.Cut(strings.TrimSpace(raw), "#")
	p = strings.TrimSpace(p)
	symbol = strings.TrimSpace(symbol)
	if p == "" || strings.HasPrefix(p, "/") || strings.Contains(p, "\\") || (hasSymbol && symbol == "") {
		return "", "", false
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return "", "", false
		}
	}
	return path.Clean(p), symbol, true
}

func cleanPath(p string) string {
	if p == "" {
		return ""
	}
	return path.Clean(strings.ReplaceAll(p, "\\", "/"))
}

// treeReader checks trace: entries against the working tree, reading and
// parsing each file at most once per build.
type treeReader struct {
	tree    fs.FS
	missing map[string]bool
	symbols map[string]map[string]bool // nil value: symbols unknown
	read    map[string]bool
}

func newTreeReader(tree fs.FS) *treeReader {
	return &treeReader{tree: tree, missing: map[string]bool{}, symbols: map[string]map[string]bool{}, read: map[string]bool{}}
}

// check returns why a trace ref does not resolve, "" when it does or when it
// cannot be checked.
func (r *treeReader) check(p, symbol string) string {
	if r.tree == nil {
		return ""
	}
	if !r.read[p] {
		r.read[p] = true
		src, err := fs.ReadFile(r.tree, p)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			r.missing[p] = true
		case err != nil:
			// A directory, or a file that cannot be read: nothing to check.
		default:
			if syms, ok := DeclaredSymbols(p, src); ok {
				set := make(map[string]bool, len(syms))
				for _, s := range syms {
					set[s] = true
				}
				r.symbols[p] = set
			}
		}
	}
	if r.missing[p] {
		return fmt.Sprintf("%s does not exist", p)
	}
	if set := r.symbols[p]; symbol != "" && set != nil && !set[symbol] {
		return fmt.Sprintf("%s declares no symbol %s", p, symbol)
	}
	return ""
}

func containsSource(ss []core.TraceSource, s core.TraceSource) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

// sortEdge orders the sources (marker before trace) and the marker lines of
// one edge.
func sortEdge(e *core.TraceEdge) {
	sort.Slice(e.Sources, func(i, j int) bool { return e.Sources[i] < e.Sources[j] })
	sort.Ints(e.Lines)
	out := e.Lines[:0]
	for i, ln := range e.Lines {
		if i == 0 || ln != e.Lines[i-1] {
			out = append(out, ln)
		}
	}
	if len(out) == 0 {
		e.Lines = nil
	} else {
		e.Lines = out
	}
}

func sortEdges(es []core.TraceEdge) {
	sort.Slice(es, func(i, j int) bool {
		a, b := es[i], es[j]
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		if a.Symbol != b.Symbol {
			return a.Symbol < b.Symbol
		}
		if c := compareRefs(a.Ref, b.Ref); c != 0 {
			return c < 0
		}
		return a.Role < b.Role
	})
}

func sortBroken(bs []core.TraceBroken) {
	sort.Slice(bs, func(i, j int) bool {
		if bs[i].Field != bs[j].Field {
			return bs[i].Field < bs[j].Field
		}
		return bs[i].Entry < bs[j].Entry
	})
}

func sortRefs(rs []core.RequirementRef) {
	sort.Slice(rs, func(i, j int) bool { return compareRefs(rs[i], rs[j]) < 0 })
}

// compareRefs orders refs by spec and then numerically by R number.
func compareRefs(a, b core.RequirementRef) int {
	switch {
	case a.Spec < b.Spec:
		return -1
	case a.Spec > b.Spec:
		return 1
	}
	return a.Number - b.Number
}

func cloneTraced(t core.TracedRequirement) core.TracedRequirement {
	t.Code = cloneEdges(t.Code)
	t.Tests = cloneEdges(t.Tests)
	t.Work = append([]core.TraceWork{}, t.Work...)
	t.Broken = append([]core.TraceBroken(nil), t.Broken...)
	return t
}

func cloneEdges(es []core.TraceEdge) []core.TraceEdge {
	out := make([]core.TraceEdge, len(es))
	for i, e := range es {
		e.Sources = append([]core.TraceSource(nil), e.Sources...)
		e.Lines = append([]int(nil), e.Lines...)
		out[i] = e
	}
	return out
}
