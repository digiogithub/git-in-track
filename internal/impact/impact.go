package impact

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/digiogithub/git-in-track/internal/core"
	"github.com/digiogithub/git-in-track/internal/gitops"
	"github.com/digiogithub/git-in-track/internal/pando"
	"github.com/digiogithub/git-in-track/internal/trace"
	"github.com/digiogithub/git-in-track/internal/vault"
)

// Defaults and bounds of the upper tiers.
const (
	// DefaultDepth is the transitive caller depth of tier 2.
	DefaultDepth = 2
	// DefaultLimit caps the callers per changed symbol (tier 2) and the
	// candidates (tier 3).
	DefaultLimit = 20
	// defaultSemanticLimit is the number of tier-3 candidates asked for when
	// the query names no limit: a few neighbors, not a ranking.
	defaultSemanticLimit = 8
	// DefaultMaxSymbols caps the changed symbols tier 2 analyzes: one Pando
	// call each. The rest are dropped, in sorted order, and the tier is
	// reported truncated.
	DefaultMaxSymbols = 25
	// DefaultCallBudget bounds the whole of tier 2.
	DefaultCallBudget = 10 * time.Second
	// maxQueryNames caps the symbol names in the tier-3 query text.
	maxQueryNames = 12
	// maxReasons caps the reasons of one hit; the rest are counted in "+n".
	maxReasons = 4
)

// Differ lists the files that changed between two revisions, or between a
// revision and the working tree (gitops.WorkingTree). gitops.Backend is one.
type Differ interface {
	ChangedFiles(ctx context.Context, from, to string) ([]gitops.FileChange, error)
}

// CommitLister resolves a revision to its commit; gitops.Backend is one. A
// Differ that is also a CommitLister lets the resolver name the diff's head
// commit, which clears the suspect flag of a requirement re-verified there
// (GIT-US-0148); without one every touched passing requirement is suspect.
type CommitLister interface {
	Commits(ctx context.Context, req gitops.LogRequest) ([]gitops.Commit, error)
}

// CallGraph is the slice of the Pando client tier 2 uses; *pando.Client is
// one.
type CallGraph interface {
	ImpactAnalysis(ctx context.Context, projectID string, symbols []string, o pando.ImpactOptions) (pando.ImpactResult, error)
}

// Coverage computes the coverage rows of requirements; *trace.Coverage is
// one (vault.RequirementCoverage).
type Coverage interface {
	Coverage(ctx context.Context, ix *core.Index, reqs []core.RequirementView) ([]core.CoverageRow, error)
}

// Options configure a Resolver.
type Options struct {
	// Engine is the trace engine of the working tree; required.
	Engine *trace.Engine
	// Differ lists the changes of a diff; required.
	Differ Differ
	// Coverage fills each hit's status and suspect; nil leaves them empty.
	Coverage Coverage
	// ProjectID is the Pando code project of the repository; empty falls
	// back to the client's default.
	ProjectID string
	// CallGraph returns the Pando client tier 2 calls, nil when there is none
	// right now. It is a function because a host can reconfigure Pando while
	// running.
	CallGraph func() CallGraph
	// Semantic returns the semantic searcher tier 3 queries with kind
	// "requirement", nil when there is none right now.
	Semantic func() vault.SemanticSearcher
	// MaxSymbols caps the symbols of tier 2; 0 is DefaultMaxSymbols.
	MaxSymbols int
	// CallBudget bounds tier 2; 0 is DefaultCallBudget.
	CallBudget time.Duration
}

// Resolver answers impact queries over one repository.
type Resolver struct {
	opts Options
}

// New returns a resolver.
func New(opts Options) *Resolver {
	if opts.MaxSymbols <= 0 {
		opts.MaxSymbols = DefaultMaxSymbols
	}
	if opts.CallBudget <= 0 {
		opts.CallBudget = DefaultCallBudget
	}
	return &Resolver{opts: opts}
}

// changedSymbol is one symbol a diff changed.
type changedSymbol struct {
	path, symbol string
}

// collector gathers the hits of every tier, keyed by requirement.
type collector struct {
	hits map[core.RequirementRef]*hitState
}

type hitState struct {
	tier      int
	score     float64
	touched   bool // a code or test edge the diff changes, directly or through a call
	behaviour bool // a reason reached the requirement through its code or the story
	tested    bool // a reason reached it through a test that verifies it
	reasons   [4][]string
}

// kind is the hit's ImpactKind: behaviour as soon as one reason is, test-only
// when every certain reason came through a verifying test, empty for a
// candidate.
func (h *hitState) kind() core.ImpactKind {
	switch {
	case h.behaviour:
		return core.ImpactKindBehaviour
	case h.tested:
		return core.ImpactKindTestOnly
	}
	return ""
}

// roleKind is the kind a trace edge's role gives a reason reaching it.
func roleKind(role core.TraceRole) core.ImpactKind {
	if role == core.TraceRoleTest {
		return core.ImpactKindTestOnly
	}
	return core.ImpactKindBehaviour
}

func (c *collector) add(ref core.RequirementRef, tier int, reason string, touched bool, kind core.ImpactKind) *hitState {
	h, ok := c.hits[ref]
	if !ok {
		h = &hitState{tier: tier}
		c.hits[ref] = h
	}
	if tier < h.tier {
		h.tier = tier
	}
	h.touched = h.touched || touched
	h.behaviour = h.behaviour || kind == core.ImpactKindBehaviour
	h.tested = h.tested || kind == core.ImpactKindTestOnly
	for _, r := range h.reasons[tier] {
		if r == reason {
			return h
		}
	}
	h.reasons[tier] = append(h.reasons[tier], reason)
	return h
}

// Impact resolves the requirements a diff affects.
func (r *Resolver) Impact(ctx context.Context, ix *core.Index, q core.ImpactQuery) (core.ImpactResult, error) {
	if r.opts.Engine == nil || r.opts.Differ == nil {
		return core.ImpactResult{}, errors.New("impact resolver: no trace engine or no git history")
	}
	res := core.ImpactResult{Base: q.Base, Head: q.Head}
	head := q.Head
	if head == "" {
		head = gitops.WorkingTree
	}
	files, err := r.opts.Differ.ChangedFiles(ctx, q.Base, head)
	if err != nil {
		var ge *gitops.Error
		if errors.As(err, &ge) && ge.Code == gitops.CodeUnknownRevision {
			return core.ImpactResult{}, fmt.Errorf("%w: %s", core.ErrUnknownRevision, ge.Message)
		}
		return core.ImpactResult{}, fmt.Errorf("list the changed files: %w", err)
	}
	res.Files = len(files)
	changes := make([]core.TraceChange, 0, len(files))
	for _, f := range files {
		tc := core.TraceChange{Path: f.Path, OldPath: f.OldPath}
		for _, l := range f.Lines {
			tc.Lines = append(tc.Lines, core.LineSpan{Start: l.Start, Count: l.Count})
		}
		changes = append(changes, tc)
	}

	col := &collector{hits: map[core.RequirementRef]*hitState{}}
	tiers := []core.ImpactTier{
		{Tier: core.ImpactTierDirect, Status: core.ImpactTierSkipped},
		{Tier: core.ImpactTierTransitive, Status: core.ImpactTierSkipped},
		{Tier: core.ImpactTierSemantic, Status: core.ImpactTierSkipped},
	}

	// Tier 1 runs first even when not asked for: TraceTouching rescans the
	// changed paths, which is what the graph of tier 2 must see.
	touching, err := r.opts.Engine.TraceTouching(ctx, ix, changes)
	if err != nil {
		return core.ImpactResult{}, fmt.Errorf("trace the changed files: %w", err)
	}
	graph, err := r.opts.Engine.Graph(ctx, ix)
	if err != nil {
		return core.ImpactResult{}, fmt.Errorf("trace graph: %w", err)
	}
	if q.Wants(core.ImpactTierDirect) {
		tiers[0].Status = core.ImpactTierOK
		for _, h := range touching {
			reason := h.Reason + ":" + h.TraceRef()
			if h.Reason == "decl" && h.Changed != "" {
				reason += " uses " + h.Changed
			}
			col.add(h.Ref, core.ImpactTierDirect, reason, true, roleKind(h.Role))
		}
		storyHits(ix, q.Story, col)
	}

	tree := r.opts.Engine.Tree()
	symbols := changedSymbols(files, changes, tree)
	res.Symbols = len(symbols)

	if q.Wants(core.ImpactTierTransitive) {
		tiers[1] = r.transitive(ctx, q, symbols, graph, tree, col)
	}
	var semantic []semanticHit
	if q.Wants(core.ImpactTierSemantic) {
		tiers[2], semantic = r.semantic(ctx, ix, q, symbols)
	}
	for _, s := range semantic {
		if _, certain := col.hits[s.ref]; certain {
			continue // never merged into the certainty of tiers 1 and 2
		}
		h := col.add(s.ref, core.ImpactTierSemantic, "semantic", false, "")
		h.score = s.score
	}

	var headSHA string
	for _, h := range col.hits {
		if h.touched {
			// Only a touched hit needs the head commit: one more diff.
			headSHA = r.headCommit(ctx, ix, q.Head)
			break
		}
	}
	hits, err := r.render(ctx, ix, col, headSHA)
	if err != nil {
		return core.ImpactResult{}, err
	}
	for _, h := range hits {
		tiers[h.Tier-1].Hits++
	}
	res.Tiers = tiers
	res.Hits = hits
	return res, nil
}

// headCommit returns the full commit id the diff ends at, "" when there is
// none to compare evidence with (GIT-US-0148, docs/03 R-IMP-5):
//
//   - a revision head is the commit it names;
//   - the working-tree head is HEAD — the commit `gintrack spec ingest`
//     records by default — but only while the working tree differs from
//     HEAD in backlog files at most (a project's .pmngr folder: a status
//     move, a comment, a verified: stamp): tests run on uncommitted code are
//     not evidence of any commit, so a tree with any other uncommitted change
//     has no head commit.
//
// Nothing here reads a clock, so the answer is a function of the history
// and the working tree alone.
func (r *Resolver) headCommit(ctx context.Context, ix *core.Index, head string) string {
	lister, ok := r.opts.Differ.(CommitLister)
	if !ok {
		return ""
	}
	commits, err := lister.Commits(ctx, gitops.LogRequest{To: head, Limit: 1})
	if err != nil || len(commits) == 0 || commits[0].SHA == "" {
		return ""
	}
	sha := commits[0].SHA
	if head == "" || head == gitops.WorkingTree {
		dirty, err := r.opts.Differ.ChangedFiles(ctx, sha, gitops.WorkingTree)
		if err != nil {
			return ""
		}
		for _, f := range dirty {
			if !isBacklogPath(ix, f.Path) || (f.OldPath != "" && !isBacklogPath(ix, f.OldPath)) {
				return ""
			}
		}
	}
	return sha
}

// isBacklogPath reports whether p lies in the backlog folder of one of the
// index's projects.
func isBacklogPath(ix *core.Index, p string) bool {
	p = path.Clean(strings.ReplaceAll(p, "\\", "/"))
	for _, pr := range ix.Projects() {
		b := path.Clean(pr.BacklogPath)
		if b == "." || b == "" {
			continue
		}
		if strings.HasPrefix(p, b+"/") {
			return true
		}
	}
	return false
}

// storyHits adds the requirements the story names in its links and its
// unapplied Spec Delta: "delta:<id>" for a pending modifies, "<kind>:<id>"
// for a declared implements or modifies. A whole-spec link names no single
// requirement and adds nothing.
func storyHits(ix *core.Index, story core.ItemID, col *collector) {
	if story == "" {
		return
	}
	for _, l := range ix.LinkGraph().Links(story) {
		if l.Kind != core.LinkImplements && l.Kind != core.LinkModifies {
			continue
		}
		ref, err := core.ParseRequirementRef(string(l.To))
		if err != nil {
			continue
		}
		if _, err := ix.Requirement(ref); err != nil {
			continue
		}
		reason := string(l.Kind) + ":" + string(story)
		if l.Pending {
			reason = "delta:" + string(story)
		}
		col.add(ref, core.ImpactTierDirect, reason, false, core.ImpactKindBehaviour)
	}
}

// changedSymbols maps the changed lines of every file still present on the
// new side to the symbols that enclose them, sorted by path and symbol.
func changedSymbols(files []gitops.FileChange, changes []core.TraceChange, tree fs.FS) []changedSymbol {
	var out []changedSymbol
	for i, f := range files {
		if f.Status == gitops.ChangeDeleted || f.Binary {
			continue
		}
		for _, s := range trace.ChangedSymbols(changes[i], tree) {
			out = append(out, changedSymbol{path: path.Clean(f.Path), symbol: s})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].path != out[j].path {
			return out[i].path < out[j].path
		}
		return out[i].symbol < out[j].symbol
	})
	return out
}

// pandoName is the name Pando resolves a trace symbol by: the last segment
// of "Type.Method", the test of "TestX/sub_case". A describe/it path is not
// a code symbol and yields "".
func pandoName(symbol string) string {
	if strings.Contains(symbol, " > ") {
		return ""
	}
	if i := strings.Index(symbol, "/"); i >= 0 {
		symbol = symbol[:i]
	}
	if i := strings.LastIndex(symbol, "."); i >= 0 {
		symbol = symbol[i+1:]
	}
	return strings.TrimSpace(symbol)
}

// isTestPath reports whether a path holds tests: nothing calls a test, so
// tier 2 does not ask Pando about one.
func isTestPath(p string) bool {
	base := path.Base(p)
	return strings.HasSuffix(base, "_test.go") || strings.Contains(base, ".test.") ||
		strings.Contains(base, ".spec.") || strings.HasPrefix(base, "test_")
}

// transitive runs tier 2: the callers of each changed symbol, mapped to the
// traced symbols that enclose them.
func (r *Resolver) transitive(ctx context.Context, q core.ImpactQuery, symbols []changedSymbol,
	graph *trace.Graph, tree fs.FS, col *collector,
) core.ImpactTier {
	tier := core.ImpactTier{Tier: core.ImpactTierTransitive, Status: core.ImpactTierOK}
	var client CallGraph
	if r.opts.CallGraph != nil {
		client = r.opts.CallGraph()
	}
	if client == nil {
		tier.Status = core.ImpactTierUnavailable
		tier.Message = "no Pando code graph is configured"
		return tier
	}
	var names []string
	seen := map[string]bool{}
	for _, s := range symbols {
		n := pandoName(s.symbol)
		if n == "" || seen[n] || isTestPath(s.path) {
			continue
		}
		seen[n] = true
		names = append(names, n)
	}
	sort.Strings(names)
	if len(names) > r.opts.MaxSymbols {
		names = names[:r.opts.MaxSymbols]
		tier.Truncated = true
	}
	depth := q.Depth
	if depth <= 0 {
		depth = DefaultDepth
	}
	limit := q.Limit
	if limit <= 0 {
		limit = DefaultLimit
	}
	ctx, cancel := context.WithTimeout(ctx, r.opts.CallBudget)
	defer cancel()

	type pending struct {
		ref    core.RequirementRef
		reason string
		kind   core.ImpactKind
	}
	var found []pending
	lines := newLineMapper(tree)
	for _, name := range names {
		// One call per symbol, so each caller is attributed to the symbol it
		// was reached from.
		out, err := client.ImpactAnalysis(ctx, r.opts.ProjectID, []string{name},
			pando.ImpactOptions{Depth: depth, Limit: limit})
		if err != nil {
			tier.Status, tier.Message = pandoFailure(err)
			tier.Truncated = false
			return tier
		}
		tier.Truncated = tier.Truncated || out.Truncated
		for _, c := range out.Callers {
			p, ok := repoPath(c.FilePath)
			if !ok {
				continue
			}
			sym := lines.symbolAt(p, c.StartLine)
			for _, e := range graph.ForSymbol(p, sym) {
				reason := "call:" + e.TraceRef() + " calls " + name + " d" + strconv.Itoa(max(c.Depth, 1))
				found = append(found, pending{ref: e.Ref, reason: reason, kind: roleKind(e.Role)})
			}
		}
	}
	for _, f := range found {
		col.add(f.ref, core.ImpactTierTransitive, f.reason, true, f.kind)
	}
	return tier
}

// pandoFailure is the status and message of a tier whose Pando call failed.
// A Pando that cannot answer is `unavailable` with a short, fixed reason
// (pando.Reason): the error's own text can quote a Pando cache id or an
// address, and two runs against one index must give byte-identical reports
// (GIT-US-0164). Any other failure is an `error` that says what it was.
func pandoFailure(err error) (status core.ImpactTierStatus, message string) {
	if pando.IsUnavailable(err) || errors.Is(err, context.DeadlineExceeded) {
		reason := pando.Reason(err)
		if reason == "" {
			reason = "Pando did not answer in time"
		}
		return core.ImpactTierUnavailable, reason
	}
	return core.ImpactTierError, err.Error()
}

// repoPath turns a Pando file path, relative to the indexed project root —
// the repository root — into a clean repository path. An absolute path or
// one that leaves the root is refused.
func repoPath(p string) (string, bool) {
	p = strings.ReplaceAll(strings.TrimSpace(p), "\\", "/")
	if p == "" || strings.HasPrefix(p, "/") {
		return "", false
	}
	p = path.Clean(p)
	if p == ".." || strings.HasPrefix(p, "../") {
		return "", false
	}
	return p, true
}

// lineMapper maps a caller's start line to the enclosing symbol with the
// scanner's own spans, reading each file once.
type lineMapper struct {
	tree  fs.FS
	files map[string][]byte
}

func newLineMapper(tree fs.FS) *lineMapper {
	return &lineMapper{tree: tree, files: map[string][]byte{}}
}

// symbolAt returns the symbol enclosing line of p; "" (the whole file) when
// the line is unknown or the file cannot be read.
func (m *lineMapper) symbolAt(p string, line int) string {
	if line <= 0 || m.tree == nil {
		return ""
	}
	src, ok := m.files[p]
	if !ok {
		var err error
		if src, err = fs.ReadFile(m.tree, p); err != nil {
			src = nil
		}
		m.files[p] = src
	}
	if src == nil {
		return ""
	}
	return trace.SymbolsAt(p, src, []int{line})[line]
}

// semanticHit is one tier-3 candidate.
type semanticHit struct {
	ref   core.RequirementRef
	score float64
}

// semantic runs tier 3: requirement blocks ranked near the changed symbol
// names and the story title.
func (r *Resolver) semantic(ctx context.Context, ix *core.Index, q core.ImpactQuery, symbols []changedSymbol) (core.ImpactTier, []semanticHit) {
	tier := core.ImpactTier{Tier: core.ImpactTierSemantic, Status: core.ImpactTierOK}
	var searcher vault.SemanticSearcher
	if r.opts.Semantic != nil {
		searcher = r.opts.Semantic()
	}
	if searcher == nil {
		tier.Status = core.ImpactTierUnavailable
		tier.Message = "no Pando semantic search is configured"
		return tier, nil
	}
	text := queryText(ix, q, symbols)
	if text == "" {
		return tier, nil
	}
	limit := q.Limit
	if limit <= 0 {
		limit = defaultSemanticLimit
	}
	hits, err := searcher.SearchSemantic(ctx, vault.SemanticQuery{Q: text, Limit: limit, Kind: core.SearchKindRequirement})
	if err != nil {
		tier.Status, tier.Message = pandoFailure(err)
		return tier, nil
	}
	var out []semanticHit
	for _, h := range hits {
		if h.Kind != core.SearchKindRequirement {
			continue
		}
		ref, err := core.ParseRequirementRef(string(h.ID))
		if err != nil {
			continue
		}
		if _, err := ix.Requirement(ref); err != nil {
			continue // another repository's requirement, or one gone since
		}
		out = append(out, semanticHit{ref: ref, score: math.Round(h.Score*1000) / 1000})
	}
	return tier, out
}

// queryText is the tier-3 query: the story title, the caller's title and the
// changed symbol names, split into words.
func queryText(ix *core.Index, q core.ImpactQuery, symbols []changedSymbol) string {
	var parts []string
	if q.Story != "" {
		if it, err := ix.Item(q.Story); err == nil {
			parts = append(parts, strings.TrimSpace(it.Title))
		}
	}
	if t := strings.TrimSpace(q.Title); t != "" {
		parts = append(parts, t)
	}
	seen := map[string]bool{}
	var names []string
	for _, s := range symbols {
		n := pandoName(s.symbol)
		if n == "" || seen[n] {
			continue
		}
		seen[n] = true
		names = append(names, n)
	}
	sort.Strings(names)
	if len(names) > maxQueryNames {
		names = names[:maxQueryNames]
	}
	parts = append(parts, names...)
	return strings.TrimSpace(strings.Join(parts, " "))
}

// render turns the collected hits into the sorted answer: title, coverage
// status, suspect, capped reasons and the pending Spec Deltas. head is the
// diff's head commit, "" when there is none.
func (r *Resolver) render(ctx context.Context, ix *core.Index, col *collector, head string) ([]core.ImpactHit, error) {
	refs := make([]core.RequirementRef, 0, len(col.hits))
	for ref := range col.hits {
		refs = append(refs, ref)
	}
	sort.Slice(refs, func(i, j int) bool {
		a, b := col.hits[refs[i]], col.hits[refs[j]]
		if a.tier != b.tier {
			return a.tier < b.tier
		}
		if refs[i].Spec != refs[j].Spec {
			return refs[i].Spec < refs[j].Spec
		}
		return refs[i].Number < refs[j].Number
	})
	views := make([]core.RequirementView, 0, len(refs))
	kept := refs[:0]
	for _, ref := range refs {
		v, err := ix.Requirement(ref)
		if err != nil {
			continue
		}
		views = append(views, v)
		kept = append(kept, ref)
	}
	refs = kept
	var rows []core.CoverageRow
	if r.opts.Coverage != nil && len(views) > 0 {
		var err error
		if rows, err = r.opts.Coverage.Coverage(ctx, ix, views); err != nil {
			return nil, fmt.Errorf("coverage of the impacted requirements: %w", err)
		}
		if len(rows) != len(views) {
			return nil, fmt.Errorf("the coverage backend answered %d of %d requirements", len(rows), len(views))
		}
	}
	graph := ix.LinkGraph()
	out := make([]core.ImpactHit, 0, len(refs))
	for i, ref := range refs {
		st := col.hits[ref]
		h := core.ImpactHit{Ref: ref, Title: views[i].Title, Tier: st.tier, Kind: st.kind()}
		if st.tier == core.ImpactTierSemantic {
			h.Candidate = true
			h.Score = st.score
		}
		if rows != nil {
			h.Status = rows[i].Status
			h.Suspect = suspect(rows[i], st.touched, head)
		}
		var reasons []string
		for t := core.ImpactTierDirect; t <= core.ImpactTierSemantic; t++ {
			rs := append([]string(nil), st.reasons[t]...)
			sort.Strings(rs)
			reasons = append(reasons, rs...)
		}
		if len(reasons) > maxReasons {
			more := len(reasons) - maxReasons
			reasons = append(reasons[:maxReasons:maxReasons], "+"+strconv.Itoa(more))
		}
		h.Reasons = reasons
		h.Pending = pendingDeltas(graph, ref)
		out = append(out, h)
	}
	return out, nil
}

// suspect decides a hit's suspect flag (docs/03 R-IMP-5): a suspect
// coverage state always is; a passing one is when the diff touches a traced
// edge of it, directly or through a call, unless its evidence verified the
// current text at the diff's head commit — every linked test passed in
// results ingested at head, or its stamp names head on the current block
// rev (CoverageRow.Commit). A failing, untested or candidate hit never is:
// failing wins, and the gate trips on it as failing.
func suspect(row core.CoverageRow, touched bool, head string) bool {
	switch row.Status {
	case core.CoverageSuspect:
		return true
	case core.CoveragePassing:
		return touched && (head == "" || row.Commit != head)
	}
	return false
}

// pendingDeltas lists the open items whose unapplied Spec Delta modifies ref.
func pendingDeltas(g *core.Graph, ref core.RequirementRef) []core.ItemID {
	if g == nil {
		return nil
	}
	var out []core.ItemID
	for _, l := range g.InverseLinks(core.ItemID(ref.String())) {
		if l.Kind == core.LinkModifiedBy && l.Pending && !containsID(out, l.To) {
			out = append(out, l.To)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func containsID(ids []core.ItemID, id core.ItemID) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}
