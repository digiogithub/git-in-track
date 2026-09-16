package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/digiogithub/git-in-track/internal/core"
	"github.com/digiogithub/git-in-track/internal/pando"
	"github.com/digiogithub/git-in-track/internal/vault"
)

// pandoBudget is the p95 latency budget of the semantic leg of a search. Pando
// scans every embedded chunk in Go with no ANN index, so a large knowledge base
// degrades gracefully instead of holding the request open: past this deadline
// the leg is abandoned and the answer is the core index alone (GIT-US-0082,
// docs/02 section 8).
const pandoBudget = 300 * time.Millisecond

// pandoSearchCap is Pando's own hard cap on kb_search_documents. Over-fetching
// up to it is how a multi-project workspace is filtered companion-side without
// asking Pando for a structured filter it does not have.
const pandoSearchCap = 20

// pandoAPI is the slice of *internal/pando.Client this package uses. It is an
// interface so that a test can drive every path — a healthy Pando, an
// unreachable one, a timeout — without a live MCP server, and so that nothing
// here depends on the transport.
type pandoAPI interface {
	// Health reports whether Pando is reachable and speaking MCP.
	Health(ctx context.Context) error
	// SearchKB returns semantic candidates from Pando's knowledge base.
	SearchKB(ctx context.Context, query string, o pando.KBSearchOptions) ([]pando.KBHit, error)
	// SearchCode returns semantic candidates from one indexed code project.
	SearchCode(ctx context.Context, projectID, query string, o pando.CodeSearchOptions) ([]pando.CodeHit, error)
	// ListProjects reports the code projects Pando already holds, which is how
	// registration knows not to register one twice.
	ListProjects(ctx context.Context) ([]pando.Project, error)
	// IndexProject queues a code index of a source tree and returns its job id.
	IndexProject(ctx context.Context, path, name string) (string, error)
	// ReindexKB re-reads the knowledge base over Pando's REST surface. It
	// answers ErrNotConfigured when no REST URL is configured.
	ReindexKB(ctx context.Context) (pando.ReindexStats, error)
	// Close ends the session.
	Close() error
}

// *pando.Client is the production implementation; the assertion keeps the
// interface honest as the client grows.
var _ pandoAPI = (*pando.Client)(nil)

// pandoSearcher turns Pando's candidates into hits of this workspace's own
// index. It satisfies vault.SemanticSearcher, which is what mounts it under
// the "search.semantic" method of the core contract.
//
// The contract it implements is deliberately narrow: Pando says *which*
// documents look relevant, and nothing else. Every field that reaches a user —
// title, path, project, id — is re-read here from the vault that owns the
// document, because Pando's copy of the front matter is at least one
// indexation behind the file (GIT-US-0096).
type pandoSearcher struct {
	client pandoAPI
	repos  *registry
	log    *slog.Logger
	// projects are the code projects this workspace's repositories are
	// registered under; the code leg searches each of them (GIT-US-0098).
	projects []codeProject
	// budget bounds the upstream call. Zero means pandoBudget.
	budget time.Duration
	// dropped counts candidates that resolved to nothing, for the log line. A
	// steady stream of them means Pando's index is stale, not that the query
	// was bad.
	dropped atomic.Int64
}

var _ vault.SemanticSearcher = (*pandoSearcher)(nil)

// SearchSemantic ranks by meaning and answers with hits of the local index.
//
// Failures are returned rather than swallowed: the caller (handleSearch) is the
// one that decides to degrade to the core index, and it has to know that it
// did.
func (p *pandoSearcher) SearchSemantic(ctx context.Context, q vault.SemanticQuery) ([]core.SearchHit, error) {
	if p == nil || p.client == nil {
		return nil, pando.ErrNotConfigured
	}
	if strings.TrimSpace(q.Q) == "" {
		return nil, nil
	}
	budget := p.budget
	if budget <= 0 {
		budget = pandoBudget
	}
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()

	// The two legs run together, inside one budget: they are independent calls
	// to the same Pando and running them in sequence would spend the deadline
	// twice (GIT-US-0098).
	var (
		code        []scoredHit
		codeDropped int
	)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		code, codeDropped = p.searchCode(ctx, q)
	}()

	// No path prefix is sent. The repository layout gives a project no prefix
	// of its own — every project's items sit side by side under `.pmngr/` —
	// and Pando reports an absolute `metadata.source_path` for some documents,
	// against which any prefix built here would match nothing at all. A filter
	// that silently returns an empty result is worse than the over-fetch the
	// scope costs, so the scope is applied below, on resolved hits.
	candidates, err := p.client.SearchKB(ctx, q.Q, pando.KBSearchOptions{Limit: overFetch(q.Limit)})
	wg.Wait()
	if err != nil {
		// The knowledge-base leg is the one the caller degrades over: it holds
		// the backlog, which is what this companion is about. A code leg that
		// failed on its own has already been logged and simply contributes
		// nothing.
		return nil, fmt.Errorf("pando kb search: %w", err)
	}

	merge := newSemanticMerge(len(candidates) + len(code))
	dropped := codeDropped
	top := topKBScore(candidates)
	for _, c := range candidates {
		hit, repo, ok := p.resolve(c, q)
		if !ok {
			dropped++
			continue
		}
		merge.add(scoredHit{hit: hit, repo: repo, rel: relative(c.Score, top)})
	}
	// Code candidates are merged after the knowledge-base ones so that a
	// document both indexes returned keeps the knowledge-base row's identity
	// when the scores tie.
	for _, hit := range code {
		merge.add(hit)
	}
	out := merge.hits(q.Limit)

	if dropped > 0 {
		p.dropped.Add(int64(dropped))
		noteSemanticDrops(ctx, dropped)
		p.log.Debug("semantic candidates dropped",
			"query", q.Q, "dropped", dropped, "kept", len(out), "total", p.dropped.Load())
	}
	return out, nil
}

// semanticDropsKey is the context key a caller hands the resolver a counter
// under.
type semanticDropsKey struct{}

// withSemanticDrops returns a context the resolver reports its dropped
// candidates into, so that a search response can say how many hits an index
// one pass behind the repository cost the answer.
//
// The counter travels in the context because two contract hops sit between the
// caller and the resolver — vault.Workspace.SearchSemantic and the
// SemanticSearcher interface — and both are the core contract's, shared with a
// browser-only build that has no semantic backend at all. Widening them for
// one backend's diagnostic would be the wrong trade (GIT-US-0096).
func withSemanticDrops(ctx context.Context, counter *atomic.Int64) context.Context {
	return context.WithValue(ctx, semanticDropsKey{}, counter)
}

// noteSemanticDrops adds to the caller's counter, if it installed one.
func noteSemanticDrops(ctx context.Context, n int) {
	if counter, ok := ctx.Value(semanticDropsKey{}).(*atomic.Int64); ok {
		counter.Add(int64(n))
	}
}

// overFetch is the limit asked of Pando: twice what the caller wants, so that
// the companion-side project filter has something left to keep, capped at what
// Pando will return at all.
func overFetch(limit int) int {
	if limit <= 0 {
		return pandoSearchCap
	}
	return min(limit*2, pandoSearchCap)
}

// pandoRef is one Pando hit taken apart: what the path says the document is,
// before anything is looked up.
type pandoRef struct {
	// kind is "item", "page" or "file", matching core.SearchHit.Kind.
	kind string
	// id is the item an item hit is, or the item a comment hit belongs to.
	id core.ItemID
	// path is the reported path, cleaned and with forward slashes.
	path string
	// comment reports that the fragment came from a comment file rather than
	// from the item itself.
	comment bool
}

// backlogDir is the directory the backlog lives in, relative to a project's
// documentation root. Pando's KBPath is that documentation root, so every
// backlog hit's path starts here (GIT-EP-0020).
const backlogDir = ".pmngr"

// commentsDir holds one directory of comment files per item, named by item id.
const commentsDir = "comments"

// parsePandoPath reads a hit's path as one of the three shapes the repository
// has: `.pmngr/<type>/<ID>-<slug>.md` is an item, `.pmngr/comments/<ID>/<file>`
// is a comment on one, and anything else is a page or a plain file — which of
// the two only the index can say, so the caller looks it up.
//
// The scan runs from the right for the `.pmngr` segment rather than anchoring
// at the start of the string, because Pando reports both a `file_path`
// relative to its KBPath and an absolute `metadata.source_path`, and a
// hand-edited KBPath puts any number of segments in front of either.
func parsePandoPath(raw string) pandoRef {
	clean := strings.TrimSpace(strings.ReplaceAll(raw, "\\", "/"))
	if clean == "" {
		return pandoRef{}
	}
	clean = path.Clean(clean)
	segments := strings.Split(strings.Trim(clean, "/"), "/")
	for i := len(segments) - 1; i >= 0; i-- {
		if segments[i] != backlogDir {
			continue
		}
		rest := segments[i+1:]
		// A comment is `comments/<ID>/<anything>`: the id is the directory
		// name, which ParseItem guarantees is the id of the item the thread
		// belongs to.
		if len(rest) >= 3 && rest[0] == commentsDir {
			if _, _, _, err := core.ParseItemID(rest[1]); err == nil {
				return pandoRef{kind: "item", id: core.ItemID(rest[1]), path: clean, comment: true}
			}
			return pandoRef{kind: "file", path: clean}
		}
		// An item is `<type>/<ID>-<slug>.md`, one directory deep, and the id
		// in the file name is the one ParseItem checked against the front
		// matter — so it is a key, not a guess.
		if len(rest) == 2 {
			if id := core.IDFromFileName(rest[1]); id != "" {
				return pandoRef{kind: "item", id: id, path: clean}
			}
		}
		return pandoRef{kind: "file", path: clean}
	}
	return pandoRef{kind: "page", path: clean}
}

// resolve maps one candidate back onto a live item, page or file of this
// workspace. It reports false for anything that no longer exists, which is how
// an index that has not been re-read since a delete never shows a dangling row.
//
// The repository the document was found in is reported alongside the hit: it is
// half of the merge key, because two mounted clones can hold the same relative
// path (GIT-US-0098).
func (p *pandoSearcher) resolve(c pando.KBHit, q vault.SemanticQuery) (core.SearchHit, string, bool) {
	var ref pandoRef
	for _, raw := range hitPaths(c) {
		ref = parsePandoPath(raw)
		if ref.kind != "" && ref.kind != "file" {
			break
		}
	}
	if ref.kind == "" {
		return core.SearchHit{}, "", false
	}
	hit := core.SearchHit{
		Kind:    ref.kind,
		Score:   c.Score,
		Snippet: chunkSnippet(c.Chunk),
		Source:  core.SearchSourcePando,
		Index:   core.SearchIndexKB,
	}
	repo := ""
	switch ref.kind {
	case "item":
		m, found := p.repos.forItem(string(ref.id))
		if !found {
			return core.SearchHit{}, "", false
		}
		it, ok := m.vlt.Item(ref.id)
		if !ok || it.Deleted {
			return core.SearchHit{}, "", false
		}
		repo = m.id
		hit.ID, hit.Path, hit.Title = it.ID, it.Path, it.Title
		if key, _, _, err := core.ParseItemID(string(it.ID)); err == nil {
			hit.Project = key
		}
		if ref.comment {
			hit.Match = core.SearchMatchComment
		}
	case "page":
		page, m, ok := p.page(ref.path)
		if ok {
			repo = m.id
			hit.Path, hit.Title, hit.Project = page.Path, page.Title, page.Project
			if hit.Project == "" {
				hit.Project = core.ProjectKey(m.id)
			}
			break
		}
		fallthrough
	case "file":
		// Neither a backlog file nor a page this index holds. Two very
		// different things look like this, and the filesystem is what tells
		// them apart: a document deleted since Pando's last pass, which is
		// dropped because a dangling row is worse than a thin result, and a
		// file that is simply not backlog and not a page, which a hand-edited
		// KBPath makes reachable and which must not vanish without a word.
		// The second carries its path and its fragment and nothing else.
		id, ok := p.onDisk(ref.path)
		if !ok {
			return core.SearchHit{}, "", false
		}
		repo = id
		hit.Kind, hit.Path = "file", ref.path
	}
	if q.Kind != "" && q.Kind != hit.Kind {
		return core.SearchHit{}, "", false
	}
	if q.Project != "" && q.Project != string(hit.Project) {
		return core.SearchHit{}, "", false
	}
	return hit, repo, true
}

// page resolves a documentation path to a live page, and names the repository
// it was found in.
//
// A relative path is the one Pando reports as `file_path`, relative to its
// KBPath — which is the documentation root, exactly what KBPage.RelPath is
// keyed by. An absolute one is `metadata.source_path`, which is resolved
// through the working tree of the mount that contains it instead.
func (p *pandoSearcher) page(rel string) (*core.KBPage, *mount, bool) {
	for _, m := range p.repos.ready() {
		if path.IsAbs(rel) {
			under, ok := underTree(m.path, rel)
			if !ok {
				continue
			}
			if page, found := m.vlt.Page(under); found {
				return page, m, true
			}
			continue
		}
		if page, found := m.vlt.PageByProjectPath("", rel); found {
			return page, m, true
		}
	}
	return nil, nil, false
}

// onDisk reports whether a hit's path names a file that is still there, and
// which mounted repository holds it. An absolute path that lies outside every
// mount still resolves, with an empty repository: a hand-edited KBPath can
// reach one, and it must not vanish without a word.
//
// It is the last question asked, over at most a handful of candidates per
// query, and only about hits the index already failed to resolve.
func (p *pandoSearcher) onDisk(rel string) (string, bool) {
	if path.IsAbs(rel) {
		info, err := os.Stat(rel)
		if err != nil || info.IsDir() {
			return "", false
		}
		for _, m := range p.repos.ready() {
			if _, ok := underTree(m.path, rel); ok {
				return m.id, true
			}
		}
		return "", true
	}
	for _, m := range p.repos.ready() {
		roots := m.docsFolders
		if len(roots) == 0 {
			roots = []string{m.docs}
		}
		for _, docs := range append(roots, "") {
			info, err := os.Stat(filepath.Join(m.path, docs, filepath.FromSlash(rel)))
			if err == nil && !info.IsDir() {
				return m.id, true
			}
		}
	}
	return "", false
}

// underTree reports the tree-relative path of an absolute one inside root.
func underTree(root, abs string) (string, bool) {
	root = path.Clean(strings.ReplaceAll(root, "\\", "/"))
	if root == "" || root == "." {
		return "", false
	}
	if !strings.HasPrefix(abs, root+"/") {
		return "", false
	}
	return strings.TrimPrefix(abs, root+"/"), true
}

// hitPaths are the paths a candidate can be resolved through, best first: the
// KBPath-relative `file_path`, then the absolute `metadata.source_path`. Pando
// reports both and which of the two is populated depends on how the document
// entered its index, so the caller tries them in turn rather than betting on
// one.
func hitPaths(c pando.KBHit) []string {
	out := make([]string, 0, 2)
	if p := strings.TrimSpace(c.FilePath); p != "" {
		out = append(out, p)
	}
	if raw, ok := c.Metadata["source_path"].(string); ok {
		if p := strings.TrimSpace(raw); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// chunkSnippet renders the matched chunk as a one-line excerpt, the same shape
// the core index produces, so the UI shows one kind of row.
func chunkSnippet(chunk string) string {
	flat := strings.Join(strings.Fields(chunk), " ")
	const maxSnippet = 240
	if len(flat) <= maxSnippet {
		return flat
	}
	cut := flat[:maxSnippet]
	if i := strings.LastIndexByte(cut, ' '); i > maxSnippet/2 {
		cut = cut[:i]
	}
	return cut + "…"
}

// notConfigured reports whether Pando is switched off rather than broken. The
// two look the same to a caller but only one of them deserves a warning.
func notConfigured(err error) bool {
	return errors.Is(err, pando.ErrNotConfigured)
}
