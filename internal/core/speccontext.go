package core

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

// The spec context of a story or a task (ADR-037, docs/03 section 21.11,
// GIT-US-0123): step 1 of the agent loop. An agent that picks up work wants
// the requirements it implements or modifies — what each one says, its
// scenarios and whether its tests pass — and the knowledge-base pages around
// them, in a few hundred tokens and generated on demand, never copied into
// the story. The collection is read from the index; coverage is computed by a
// native host and attached by the vault ("spec.context"); the rendering shares
// the token estimator, the cursor and the page fitting of the impact report
// (budget.go), so a budget means the same thing in both.

// How a requirement reached the context: the item's declared links, or its
// unapplied Spec Delta.
const (
	SpecViaImplements = "implements"       // a declared implements link
	SpecViaModifies   = "modifies"         // a declared modifies link
	SpecViaAdded      = "delta-added"      // an ADDED block of the Spec Delta
	SpecViaModified   = "delta-modified"   // a MODIFIED operation of the Spec Delta
	SpecViaRemoved    = "delta-removed"    // a REMOVED operation of the Spec Delta
	SpecViaSuperseded = "delta-superseded" // the Supersedes: target of an ADDED block
)

// Clip widths of the spec context. A statement is one line: its first
// sentence or two, never the whole block.
const (
	specContextStatementWidth = 160
	specContextStepWidth      = 100
	specContextTitleWidth     = 60
	// specContextMaxPages bounds the related knowledge-base pages.
	specContextMaxPages = 10
)

// Coverage states of the whole context, as the vault attaches them.
const (
	SpecCoverageOK          = "ok"
	SpecCoverageUnavailable = "unavailable"
)

// SpecContextScenario is one scenario of a requirement: its name, and its
// steps only when the page's budget allows them.
type SpecContextScenario struct {
	Name  string   `json:"name"`
	Steps []string `json:"steps,omitempty"`
}

// SpecContextRequirement is one requirement of a spec context.
type SpecContextRequirement struct {
	// Ref is the requirement ref; for an ADDED block not yet numbered it is
	// the spec id the block will join.
	Ref   string `json:"ref"`
	Title string `json:"title"`
	// Via says how the item reaches it, in the order the kinds were found.
	Via []string `json:"via"`
	// WholeSpec marks a requirement reached through a link to its whole spec.
	WholeSpec bool `json:"wholeSpec,omitempty"`
	// Proposed marks a statement and scenarios read from the item's Spec
	// Delta rather than from the spec: what the requirement will say.
	Proposed bool `json:"proposed,omitempty"`
	// Statement is the requirement's statement on one line, clipped.
	Statement string                `json:"statement,omitempty"`
	Scenarios []SpecContextScenario `json:"scenarios,omitempty"`
	// Status and Reasons are the coverage row of the requirement, when a
	// coverage backend answered.
	Status  CoverageStatus `json:"status,omitempty"`
	Reasons []string       `json:"reasons,omitempty"`
}

// SpecContextPage is one related knowledge-base page.
type SpecContextPage struct {
	Path  string `json:"path"`
	Title string `json:"title,omitempty"`
}

// SpecContext is the whole context of one story or task, before a budget.
type SpecContext struct {
	Item  ItemID `json:"item"`
	Title string `json:"title"`
	// Coverage is ok when a coverage backend answered and unavailable when
	// the session has none (browser-only mode); empty until the vault sets it.
	Coverage     string                   `json:"coverage,omitempty"`
	Requirements []SpecContextRequirement `json:"requirements"`
	Pages        []SpecContextPage        `json:"pages,omitempty"`
	// MorePages counts the related pages past specContextMaxPages.
	MorePages int `json:"morePages,omitempty"`
}

// Refs returns the numbered requirements of the context, in order: the ones
// a coverage backend can answer for.
func (c SpecContext) Refs() []RequirementRef {
	var out []RequirementRef
	for _, r := range c.Requirements {
		if ref, err := ParseRequirementRef(r.Ref); err == nil {
			out = append(out, ref)
		}
	}
	return out
}

// ApplyCoverage attaches coverage rows to the requirements they name and marks
// the context's coverage as answered.
func (c *SpecContext) ApplyCoverage(rows []CoverageRow) {
	byRef := make(map[string]CoverageRow, len(rows))
	for _, row := range rows {
		byRef[row.Ref.String()] = row
	}
	for i := range c.Requirements {
		if row, ok := byRef[c.Requirements[i].Ref]; ok {
			c.Requirements[i].Status = row.Status
			c.Requirements[i].Reasons = row.Reasons
		}
	}
	c.Coverage = SpecCoverageOK
}

// SpecContext collects the context of one story or task: the requirements
// its implements and modifies links name (a link to a whole spec names every
// requirement of it), the targets of its unapplied Spec Delta — ADDED blocks
// not yet numbered included — and the knowledge-base pages the item and those
// specs link with wikilinks. Requirements are ordered by spec, then number,
// with a spec's unnumbered ADDED blocks after its numbered ones in delta order.
// A link or delta target the index does not hold is left out (the validator
// reports it as W-DELTA-DANGLING or a broken link). Coverage is not computed
// here: see ApplyCoverage.
func (ix *Index) SpecContext(id ItemID) (SpecContext, error) {
	ix.mu.RLock()
	it, ok := ix.byID[id]
	if !ok {
		ix.mu.RUnlock()
		return SpecContext{}, fmt.Errorf("spec context of %s: %w", id, ErrItemNotFound)
	}
	graph := ix.graph
	delta, hasDelta := ix.deltas[id]
	if hasDelta {
		// A done item's delta was applied, and its links carry it; a
		// cancelled item's delta proposes nothing (R-DELTA-4).
		switch ix.categoryOf(it) {
		case CategoryDone, CategoryCancelled:
			hasDelta = false
		}
	}
	out := SpecContext{Item: it.ID, Title: it.Title, Requirements: []SpecContextRequirement{}}
	ix.mu.RUnlock()

	c := &contextCollector{ix: ix, byKey: map[string]int{}}
	for _, l := range graph.Links(id) {
		if l.Pending || (l.Kind != LinkImplements && l.Kind != LinkModifies) {
			continue
		}
		target := bareTarget(string(l.To))
		if ref, err := ParseRequirementRef(target); err == nil {
			c.existing(ref, string(l.Kind), false)
			continue
		}
		views, err := ix.Requirements(RequirementFilter{Spec: ItemID(target)})
		if err != nil {
			return SpecContext{}, fmt.Errorf("spec context of %s: %w", id, err)
		}
		for _, v := range views {
			c.existing(v.Ref, string(l.Kind), true)
		}
	}
	if hasDelta {
		for i, op := range delta.Operations {
			switch {
			case op.Op == DeltaAdded && op.Ref == nil:
				c.proposed(op, i)
			case op.Op == DeltaAdded:
				c.existing(*op.Ref, SpecViaAdded, false)
				c.propose(op.Ref.String(), op)
			case op.Op == DeltaModified && op.Ref != nil:
				c.existing(*op.Ref, SpecViaModified, false)
				c.propose(op.Ref.String(), op)
			case op.Op == DeltaRemoved && op.Ref != nil:
				c.existing(*op.Ref, SpecViaRemoved, false)
			}
			if op.Supersedes != nil {
				c.existing(*op.Supersedes, SpecViaSuperseded, false)
			}
		}
	}
	sort.SliceStable(c.entries, func(i, j int) bool {
		a, b := c.entries[i], c.entries[j]
		if a.spec != b.spec {
			return a.spec < b.spec
		}
		if (a.number == 0) != (b.number == 0) {
			return a.number != 0
		}
		if a.number != b.number {
			return a.number < b.number
		}
		return a.order < b.order
	})
	for _, e := range c.entries {
		out.Requirements = append(out.Requirements, e.req)
	}

	// Related pages: the wikilinks of the item, then of each spec involved.
	nodes := []NodeID{ItemNode(id)}
	seenSpec := map[ItemID]bool{}
	for _, e := range c.entries {
		if !seenSpec[e.spec] {
			seenSpec[e.spec] = true
			nodes = append(nodes, ItemNode(e.spec))
		}
	}
	seenPage := map[string]bool{}
	for _, n := range nodes {
		for _, r := range graph.References(n) {
			if !r.Resolved || r.To.Kind() != "page" || seenPage[r.To.Value()] {
				continue
			}
			p := r.To.Value()
			seenPage[p] = true
			if len(out.Pages) == specContextMaxPages {
				out.MorePages++
				continue
			}
			page := SpecContextPage{Path: p}
			if kb, ok := ix.Page(p); ok {
				page.Title = kb.Title
			}
			out.Pages = append(out.Pages, page)
		}
	}
	return out, nil
}

// contextCollector gathers the requirements of a context, once each.
type contextCollector struct {
	ix      *Index
	byKey   map[string]int
	entries []contextEntry
}

type contextEntry struct {
	spec   ItemID
	number int // 0 for an unnumbered ADDED block
	order  int // delta order of an unnumbered ADDED block
	req    SpecContextRequirement
}

// existing adds a requirement the index holds, or one more way to reach it.
func (c *contextCollector) existing(ref RequirementRef, via string, wholeSpec bool) {
	key := ref.String()
	if i, ok := c.byKey[key]; ok {
		e := &c.entries[i]
		if !containsString(e.req.Via, via) {
			e.req.Via = append(e.req.Via, via)
		}
		e.req.WholeSpec = e.req.WholeSpec && wholeSpec
		return
	}
	v, err := c.ix.Requirement(ref)
	if err != nil {
		return
	}
	c.byKey[key] = len(c.entries)
	c.entries = append(c.entries, contextEntry{spec: ref.Spec, number: ref.Number, req: SpecContextRequirement{
		Ref: key, Title: v.Title, Via: []string{via}, WholeSpec: wholeSpec,
		Statement: clipText(v.Statement, specContextStatementWidth), Scenarios: contextScenarios(v.Scenarios),
	}})
}

// propose replaces what a requirement says with its Spec Delta proposal, when
// the operation proposes a text.
func (c *contextCollector) propose(key string, op DeltaOperation) {
	i, ok := c.byKey[key]
	if !ok || (op.Statement == "" && len(op.Scenarios) == 0) {
		return
	}
	e := &c.entries[i]
	e.req.Proposed = true
	e.req.Statement = clipText(op.Statement, specContextStatementWidth)
	e.req.Scenarios = contextScenarios(op.Scenarios)
	if t := strings.TrimSpace(op.Title); t != "" {
		e.req.Title = t
	}
}

// proposed adds an ADDED block that has no number yet: it is addressed by the
// spec it will join.
func (c *contextCollector) proposed(op DeltaOperation, order int) {
	c.entries = append(c.entries, contextEntry{spec: op.Spec, order: order, req: SpecContextRequirement{
		Ref: string(op.Spec), Title: strings.TrimSpace(op.Title), Via: []string{SpecViaAdded}, Proposed: true,
		Statement: clipText(op.Statement, specContextStatementWidth), Scenarios: contextScenarios(op.Scenarios),
	}})
}

func contextScenarios(in []Scenario) []SpecContextScenario {
	if len(in) == 0 {
		return nil
	}
	out := make([]SpecContextScenario, 0, len(in))
	for _, sc := range in {
		steps := make([]string, 0, len(sc.Steps))
		for _, st := range sc.Steps {
			steps = append(steps, clipText(st, specContextStepWidth))
		}
		if len(steps) == 0 {
			steps = nil
		}
		out = append(out, SpecContextScenario{Name: sc.Name, Steps: steps})
	}
	return out
}

// SpecContextDetail is how much of each scenario a page carries.
type SpecContextDetail string

// The two levels, richest first.
const (
	// SpecDetailSteps: scenario names and their steps.
	SpecDetailSteps SpecContextDetail = "steps"
	// SpecDetailNames: scenario names only.
	SpecDetailNames SpecContextDetail = "names"
)

// SpecContextReport is one page of the token-budgeted spec context.
type SpecContextReport struct {
	Item     ItemID `json:"item"`
	Title    string `json:"title"`
	Coverage string `json:"coverage,omitempty"`
	// Detail is the scenario detail of this page: steps when the whole page
	// fits with them, names otherwise.
	Detail       SpecContextDetail        `json:"detail"`
	Requirements []SpecContextRequirement `json:"requirements,omitempty"`
	// Pages are the related knowledge-base pages, on the first page only.
	Pages     []SpecContextPage `json:"pages,omitempty"`
	MorePages int               `json:"morePages,omitempty"`
	// Text is the text form of the page.
	Text string `json:"text,omitempty"`
	// Total counts every requirement; Offset is where this page starts.
	Total  int `json:"total"`
	Offset int `json:"offset,omitempty"`
	// Truncated counts the requirements after this page; NextCursor fetches
	// them.
	Truncated  int    `json:"truncated,omitempty"`
	NextCursor string `json:"nextCursor,omitempty"`
	// Budget is the budget the page was cut at; Tokens is the page's own
	// estimate, never above Budget unless a single requirement alone
	// exceeds it.
	Budget int `json:"budget"`
	Tokens int `json:"tokens"`
}

// specContextFingerprint hashes what a cursor offset depends on: the item and
// its ordered requirements.
func specContextFingerprint(c SpecContext) string {
	var b strings.Builder
	b.WriteString(string(c.Item) + "\x00")
	for _, r := range c.Requirements {
		b.WriteString(r.Ref + "\x00" + r.Title + "\n")
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])[:8]
}

// RenderSpecContext renders one page of a spec context within a token
// budget. It degrades in a fixed order: the page first drops scenario steps
// (they are kept only when every remaining requirement fits with them), then
// cuts the requirement list, handing back a cursor for the rest. The one-line
// statement, the scenario names, the coverage status and reasons of a
// requirement on the page are never dropped, and the related pages ride on the
// first page only. The only error is a cursor that does not belong to this
// context (ErrInvalidCursor) or an unknown format.
func RenderSpecContext(c SpecContext, opt ImpactReportOptions) (SpecContextReport, error) {
	budget, format, err := opt.resolve()
	if err != nil {
		return SpecContextReport{}, err
	}
	filter := specContextFingerprint(c)
	offset, err := decodeReportCursor(opt.Cursor, filter, len(c.Requirements), "spec context")
	if err != nil {
		return SpecContextReport{}, err
	}
	rest := c.Requirements[offset:]

	render := func(n int, detail SpecContextDetail) (SpecContextReport, int) {
		r := SpecContextReport{
			Item: c.Item, Title: c.Title, Coverage: c.Coverage, Detail: detail,
			Total: len(c.Requirements), Offset: offset, Budget: budget,
		}
		if offset == 0 {
			r.Pages, r.MorePages = c.Pages, c.MorePages
		}
		if left := len(rest) - n; left > 0 {
			r.Truncated = left
			r.NextCursor = encodeReportCursor(offset+n, filter)
		}
		reqs := rest[:n]
		if detail == SpecDetailNames {
			reqs = withoutSteps(reqs)
		}
		if format == ImpactReportText {
			r.Text = specContextText(r, reqs)
			r.Pages, r.MorePages = nil, 0
		} else {
			r.Requirements = reqs
		}
		cost := settleTokens(&r, &r.Tokens)
		return r, cost
	}
	if full, cost := render(len(rest), SpecDetailSteps); cost <= budget {
		return full, nil
	}
	return fitBudget(len(rest), budget, func(n int) (SpecContextReport, int) {
		return render(n, SpecDetailNames)
	}), nil
}

// withoutSteps copies the requirements with their scenario names only.
func withoutSteps(in []SpecContextRequirement) []SpecContextRequirement {
	out := make([]SpecContextRequirement, len(in))
	for i, r := range in {
		if len(r.Scenarios) > 0 {
			names := make([]SpecContextScenario, len(r.Scenarios))
			for j, sc := range r.Scenarios {
				names[j] = SpecContextScenario{Name: sc.Name}
			}
			r.Scenarios = names
		}
		out[i] = r
	}
	return out
}

// specContextText renders the text form of a page:
//
//	context <item> "<title>": <total> requirements, coverage <ok|unavailable>[, showing a-b]
//	<ref> <via,...> <status|-> [proposed] "<title>" [<reason> [+n]]
//	  <statement>
//	  scenarios: <name>; <name>          (names detail)
//	  scenario <name>: <step> / <step>   (steps detail, one line per scenario)
//	kb: <path> "<title>"; ... [+n]        (first page only)
//	truncated: <n>, cursor: <token>
func specContextText(r SpecContextReport, reqs []SpecContextRequirement) string {
	var b strings.Builder
	coverage := r.Coverage
	if coverage == "" {
		coverage = SpecCoverageUnavailable
	}
	fmt.Fprintf(&b, "context %s %q: %d requirements, coverage %s", r.Item,
		clipText(r.Title, specContextTitleWidth), r.Total, coverage)
	if r.Offset > 0 || r.Truncated > 0 {
		fmt.Fprintf(&b, ", showing %d-%d", r.Offset+1, r.Offset+len(reqs))
	}
	b.WriteByte('\n')
	for _, q := range reqs {
		status := string(q.Status)
		if status == "" {
			status = "-"
		}
		fmt.Fprintf(&b, "%s %s %s", q.Ref, strings.Join(q.Via, ","), status)
		if q.Proposed {
			b.WriteString(" proposed")
		}
		fmt.Fprintf(&b, " %q", clipText(q.Title, specContextTitleWidth))
		if len(q.Reasons) > 0 {
			b.WriteString(" " + clipText(q.Reasons[0], impactReasonWidth))
			if more := len(q.Reasons) - 1; more > 0 {
				fmt.Fprintf(&b, " +%d", more)
			}
		}
		b.WriteByte('\n')
		if q.Statement != "" {
			b.WriteString("  " + q.Statement + "\n")
		}
		if len(q.Scenarios) == 0 {
			continue
		}
		if r.Detail == SpecDetailSteps {
			for _, sc := range q.Scenarios {
				b.WriteString("  scenario " + clipText(sc.Name, specContextTitleWidth))
				if len(sc.Steps) > 0 {
					b.WriteString(": " + strings.Join(sc.Steps, " / "))
				}
				b.WriteByte('\n')
			}
			continue
		}
		names := make([]string, len(q.Scenarios))
		for i, sc := range q.Scenarios {
			names[i] = clipText(sc.Name, specContextTitleWidth)
		}
		b.WriteString("  scenarios: " + strings.Join(names, "; ") + "\n")
	}
	if r.Offset == 0 && len(r.Pages) > 0 {
		parts := make([]string, len(r.Pages))
		for i, p := range r.Pages {
			parts[i] = p.Path
			if p.Title != "" {
				parts[i] += fmt.Sprintf(" %q", clipText(p.Title, specContextTitleWidth))
			}
		}
		b.WriteString("kb: " + strings.Join(parts, "; "))
		if r.MorePages > 0 {
			fmt.Fprintf(&b, " +%d", r.MorePages)
		}
		b.WriteByte('\n')
	}
	if r.Truncated > 0 {
		fmt.Fprintf(&b, "truncated: %d, cursor: %s\n", r.Truncated, r.NextCursor)
	}
	return b.String()
}
