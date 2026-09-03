package core

import (
	"sort"
	"strings"
)

// NodeID addresses one node of the link graph. Items are "item:<ID>" and pages
// are "page:<vault-relative path>", which keeps the two namespaces in one map
// without a struct key.
type NodeID string

// ItemNode returns the graph node of a backlog item.
func ItemNode(id ItemID) NodeID { return NodeID("item:" + id) }

// PageNode returns the graph node of a knowledge-base page.
func PageNode(path string) NodeID { return NodeID("page:" + path) }

// Kind reports whether the node is an item or a page.
func (n NodeID) Kind() string {
	s := string(n)
	if i := strings.Index(s, ":"); i >= 0 {
		return s[:i]
	}
	return ""
}

// Value returns the item id or the page path the node stands for.
func (n NodeID) Value() string {
	s := string(n)
	if i := strings.Index(s, ":"); i >= 0 {
		return s[i+1:]
	}
	return s
}

// GraphLink is one typed relation edge. Computed reports an edge the index
// derived from the inverse kind: links are stored on one side only (R-LINK-1).
type GraphLink struct {
	Kind     LinkKind `json:"kind"`
	From     ItemID   `json:"from"`
	To       ItemID   `json:"to"`
	Note     string   `json:"note,omitempty"`
	Computed bool     `json:"computed,omitempty"`
}

// Reference is one wikilink edge between two nodes. Resolved is false when the
// target could not be found, which is a warning and never an error (R-WIKI-2).
type Reference struct {
	From     NodeID     `json:"from"`
	To       NodeID     `json:"to,omitempty"`
	Target   string     `json:"target"`
	Anchor   string     `json:"anchor,omitempty"`
	Text     string     `json:"text,omitempty"`
	Resolved bool       `json:"resolved"`
	Project  ProjectKey `json:"project,omitempty"`
}

// Graph is the read-only projection of every relation the index knows about:
// the parent/child hierarchy, the typed links with their computed inverses, and
// the wikilink references between pages and items with their backlinks
// (docs/03 section 14.2).
//
// A Graph is immutable once built; the index replaces it wholesale.
type Graph struct {
	parent    map[ItemID]ItemID
	children  map[ItemID][]ItemID
	milestone map[ItemID][]ItemID
	out       map[ItemID][]GraphLink
	in        map[ItemID][]GraphLink
	refs      map[NodeID][]Reference
	backrefs  map[NodeID][]Reference
}

// newGraph returns an empty graph ready to be filled by the index.
func newGraph() *Graph {
	return &Graph{
		parent:    make(map[ItemID]ItemID),
		children:  make(map[ItemID][]ItemID),
		milestone: make(map[ItemID][]ItemID),
		out:       make(map[ItemID][]GraphLink),
		in:        make(map[ItemID][]GraphLink),
		refs:      make(map[NodeID][]Reference),
		backrefs:  make(map[NodeID][]Reference),
	}
}

// addParent records a hierarchy edge from a child to its parent.
func (g *Graph) addParent(child, parent ItemID) {
	if child == "" || parent == "" {
		return
	}
	g.parent[child] = parent
	g.children[parent] = append(g.children[parent], child)
}

// addMilestone records an item as belonging to a milestone.
func (g *Graph) addMilestone(item, milestone ItemID) {
	if item == "" || milestone == "" {
		return
	}
	g.milestone[milestone] = append(g.milestone[milestone], item)
}

// addLink records a declared relation and its computed inverse.
func (g *Graph) addLink(from ItemID, l Link) {
	target := ItemID(bareTarget(l.Target))
	if from == "" || target == "" || !l.Kind.Valid() {
		return
	}
	g.out[from] = append(g.out[from], GraphLink{Kind: l.Kind, From: from, To: target, Note: l.Note})
	g.in[target] = append(g.in[target], GraphLink{
		Kind: l.Kind.Inverse(), From: target, To: from, Note: l.Note, Computed: true,
	})
}

// addReference records a wikilink edge and its backlink.
func (g *Graph) addReference(r Reference) {
	g.refs[r.From] = append(g.refs[r.From], r)
	if r.Resolved && r.To != "" {
		g.backrefs[r.To] = append(g.backrefs[r.To], r)
	}
}

// finish sorts every adjacency list so that two builds of the same tree produce
// the same graph, and drops duplicate edges.
func (g *Graph) finish() {
	for k, v := range g.children {
		sort.Slice(v, func(i, j int) bool { return v[i] < v[j] })
		g.children[k] = dedupIDs(v)
	}
	for k, v := range g.milestone {
		sort.Slice(v, func(i, j int) bool { return v[i] < v[j] })
		g.milestone[k] = dedupIDs(v)
	}
	for k, v := range g.out {
		g.out[k] = sortLinks(v)
	}
	for k, v := range g.in {
		g.in[k] = sortLinks(v)
	}
	for k, v := range g.refs {
		g.refs[k] = sortReferences(v)
	}
	for k, v := range g.backrefs {
		g.backrefs[k] = sortReferences(v)
	}
}

// Parent returns the parent of an item.
func (g *Graph) Parent(id ItemID) (ItemID, bool) {
	p, ok := g.parent[id]
	return p, ok
}

// Children returns the direct children of an item, sorted by id.
func (g *Graph) Children(id ItemID) []ItemID { return append([]ItemID(nil), g.children[id]...) }

// MilestoneItems returns the items assigned to a milestone, sorted by id.
func (g *Graph) MilestoneItems(id ItemID) []ItemID {
	return append([]ItemID(nil), g.milestone[id]...)
}

// Links returns the relations declared on an item.
func (g *Graph) Links(id ItemID) []GraphLink { return append([]GraphLink(nil), g.out[id]...) }

// InverseLinks returns the relations that point at an item, computed from the
// links declared elsewhere.
func (g *Graph) InverseLinks(id ItemID) []GraphLink { return append([]GraphLink(nil), g.in[id]...) }

// AllLinks returns the declared relations and the computed inverses together,
// which is what a "relations" panel shows.
func (g *Graph) AllLinks(id ItemID) []GraphLink {
	out := make([]GraphLink, 0, len(g.out[id])+len(g.in[id]))
	out = append(out, g.out[id]...)
	out = append(out, g.in[id]...)
	return sortLinks(out)
}

// Blocking returns the items this item blocks, declared or inferred.
func (g *Graph) Blocking(id ItemID) []ItemID { return g.targetsOfKind(id, LinkBlocks) }

// BlockedBy returns the items that block this one, declared or inferred.
func (g *Graph) BlockedBy(id ItemID) []ItemID { return g.targetsOfKind(id, LinkBlockedBy) }

func (g *Graph) targetsOfKind(id ItemID, kind LinkKind) []ItemID {
	var out []ItemID
	for _, l := range g.AllLinks(id) {
		if l.Kind == kind {
			out = append(out, l.To)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return dedupIDs(out)
}

// References returns the wikilinks that leave a node.
func (g *Graph) References(n NodeID) []Reference { return append([]Reference(nil), g.refs[n]...) }

// Backlinks returns the wikilinks that point at a node, from pages and from
// item bodies alike.
func (g *Graph) Backlinks(n NodeID) []Reference { return append([]Reference(nil), g.backrefs[n]...) }

// Nodes returns every node that has at least one outgoing or incoming
// reference, sorted. It exists for tests and for diagnostics.
func (g *Graph) Nodes() []NodeID {
	seen := make(map[NodeID]bool, len(g.refs)+len(g.backrefs))
	for n := range g.refs {
		seen[n] = true
	}
	for n := range g.backrefs {
		seen[n] = true
	}
	out := make([]NodeID, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// bareTarget strips the "<KEY>/" qualifier of a cross-project link target
// (R-LINK-2) and returns the item id part.
func bareTarget(target string) string {
	t := strings.TrimSpace(target)
	if i := strings.Index(t, "/"); i > 0 {
		if ValidProjectKey(ProjectKey(t[:i])) {
			return t[i+1:]
		}
	}
	return t
}

// targetProject returns the project a link target belongs to: the qualifier when
// present, otherwise the key embedded in the item id.
func targetProject(target string) ProjectKey {
	t := strings.TrimSpace(target)
	if i := strings.Index(t, "/"); i > 0 {
		if key := ProjectKey(t[:i]); ValidProjectKey(key) {
			return key
		}
	}
	if key, _, _, err := ParseItemID(t); err == nil {
		return key
	}
	return ""
}

func dedupIDs(in []ItemID) []ItemID {
	if len(in) < 2 {
		return in
	}
	out := in[:1]
	for _, v := range in[1:] {
		if v != out[len(out)-1] {
			out = append(out, v)
		}
	}
	return out
}

func sortLinks(in []GraphLink) []GraphLink {
	sort.SliceStable(in, func(i, j int) bool {
		a, b := in[i], in[j]
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.To != b.To {
			return a.To < b.To
		}
		return !a.Computed && b.Computed
	})
	// The same relation may be declared on both sides, which yields a declared
	// edge and a computed one; the declared edge sorts first and wins.
	out := in[:0]
	var prev GraphLink
	for i, l := range in {
		if i > 0 && l.Kind == prev.Kind && l.To == prev.To && l.From == prev.From {
			continue
		}
		prev = l
		out = append(out, l)
	}
	return out
}

func sortReferences(in []Reference) []Reference {
	sort.SliceStable(in, func(i, j int) bool {
		a, b := in[i], in[j]
		if a.From != b.From {
			return a.From < b.From
		}
		if a.To != b.To {
			return a.To < b.To
		}
		if a.Target != b.Target {
			return a.Target < b.Target
		}
		return a.Anchor < b.Anchor
	})
	out := in[:0]
	var prev Reference
	for i, r := range in {
		if i > 0 && r.From == prev.From && r.To == prev.To && r.Target == prev.Target && r.Anchor == prev.Anchor {
			continue
		}
		prev = r
		out = append(out, r)
	}
	return out
}
