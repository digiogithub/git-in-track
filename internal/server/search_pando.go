package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path"
	"strings"
	"sync/atomic"
	"time"

	"github.com/digiogithub/git-in-track/internal/core"
	"github.com/digiogithub/git-in-track/internal/pando"
	"github.com/digiogithub/git-in-track/internal/vault"
)

// pandoBudget is the p95 latency budget of the semantic leg of a search. Pando
// scans every embedded chunk of the corpus in Go with no ANN index, so a large
// corpus degrades gracefully instead of holding the request open: past this
// deadline the leg is abandoned and the answer is the core index alone
// (GIT-US-0082, docs/02 section 8).
const pandoBudget = 300 * time.Millisecond

// pandoSearchCap is Pando's own hard cap on kb_search_documents. Over-fetching
// up to it is how a multi-project corpus is filtered companion-side without
// asking Pando for a structured filter it does not have.
const pandoSearchCap = 20

// pandoAPI is the slice of *internal/pando.Client this package uses. It is an
// interface so that a test can drive every path — a healthy Pando, an
// unreachable one, a timeout — without a live MCP server, and so that nothing
// here depends on the transport.
type pandoAPI interface {
	// Health reports whether Pando is reachable and speaking MCP.
	Health(ctx context.Context) error
	// SearchKB returns semantic candidates from the exported corpus.
	SearchKB(ctx context.Context, query string, o pando.KBSearchOptions) ([]pando.KBHit, error)
	// IndexProject queues a code index of a source tree and returns its job id.
	IndexProject(ctx context.Context, path, name string) (string, error)
	// ReindexKB re-imports the corpus over Pando's REST surface. It answers
	// ErrNotConfigured when no REST URL is configured.
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
// document, because the corpus keeps only `tags` and `aliases` of the front
// matter and its copy is always at least one export behind (GIT-US-0073).
type pandoSearcher struct {
	client pandoAPI
	repos  *registry
	log    *slog.Logger
	// budget bounds the upstream call. Zero means pandoBudget.
	budget time.Duration
	// dropped counts candidates that resolved to nothing, for the log line. A
	// steady stream of them means the corpus is stale, not that the query was
	// bad.
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

	candidates, err := p.client.SearchKB(ctx, q.Q, pando.KBSearchOptions{
		Limit:      overFetch(q.Limit),
		PathPrefix: corpusPrefix(q.Project, q.Kind),
	})
	if err != nil {
		return nil, fmt.Errorf("pando kb search: %w", err)
	}

	out := make([]core.SearchHit, 0, len(candidates))
	seen := make(map[string]bool, len(candidates))
	dropped := 0
	for _, c := range candidates {
		hit, ok := p.resolve(c, q)
		if !ok {
			dropped++
			continue
		}
		key := hit.Kind + "\x00" + string(hit.ID) + "\x00" + hit.Path
		if seen[key] {
			// A document is chunked, so the same file can come back several
			// times with different chunks. The best-ranked chunk wins.
			continue
		}
		seen[key] = true
		out = append(out, hit)
		if q.Limit > 0 && len(out) >= q.Limit {
			break
		}
	}
	if dropped > 0 {
		p.dropped.Add(int64(dropped))
		p.log.Debug("semantic candidates dropped",
			"query", q.Q, "dropped", dropped, "kept", len(out), "total", p.dropped.Load())
	}
	return out, nil
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

// corpusPrefix is the path prefix a scoped query sends to Pando. The corpus is
// laid out as `<PROJECT>/items/<ID>.md` and `<PROJECT>/kb/<path>.md`
// (internal/pandosync), so a project scope is one prefix and a kind scope
// narrows it by one more segment.
//
// An unscoped query sends no prefix: Pando's own cap plus the resolution below
// is the filter. A corpus holding several projects can therefore under-return
// for a scoped query whose best matches all live in another project — the
// documented cost of Pando having no structured filter (GIT-US-0082).
func corpusPrefix(project, kind string) string {
	if project == "" {
		return ""
	}
	switch kind {
	case "item":
		return project + "/items/"
	case "page":
		return project + "/kb/"
	default:
		return project + "/"
	}
}

// resolve maps one candidate back onto a live item or page. It reports false
// for anything that no longer exists, which is how a corpus that has not been
// re-exported since a delete never shows a dangling row.
func (p *pandoSearcher) resolve(c pando.KBHit, q vault.SemanticQuery) (core.SearchHit, bool) {
	ref, ok := parseCorpusPath(c.FilePath)
	if !ok {
		return core.SearchHit{}, false
	}
	if q.Kind != "" && q.Kind != ref.kind {
		return core.SearchHit{}, false
	}
	if q.Project != "" && q.Project != ref.project {
		return core.SearchHit{}, false
	}
	m, found := p.repos.forProject(ref.project)
	if !found {
		return core.SearchHit{}, false
	}
	hit := core.SearchHit{
		Kind:    ref.kind,
		Project: core.ProjectKey(ref.project),
		Score:   c.Score,
		Snippet: chunkSnippet(c.Chunk),
		Source:  core.SearchSourcePando,
	}
	switch ref.kind {
	case "item":
		it, ok := m.vlt.Item(core.ItemID(ref.rest))
		if !ok || it.Deleted {
			return core.SearchHit{}, false
		}
		hit.ID, hit.Path, hit.Title = it.ID, it.Path, it.Title
	case "page":
		page, ok := m.vlt.PageByProjectPath(core.ProjectKey(ref.project), ref.rest)
		if !ok {
			return core.SearchHit{}, false
		}
		hit.Path, hit.Title = page.Path, page.Title
		if page.Project != "" {
			hit.Project = page.Project
		}
	default:
		return core.SearchHit{}, false
	}
	return hit, true
}

// corpusRef is one exported document, taken apart.
type corpusRef struct {
	// kind is "item" or "page", matching core.SearchHit.Kind.
	kind string
	// project is the project key the document was filed under.
	project string
	// rest is the item id for an item, and the documentation-folder relative
	// path (extension included) for a page.
	rest string
}

// parseCorpusPath reads `<PROJECT>/items/<ID>.md` and `<PROJECT>/kb/<path>.md`
// back into their parts.
//
// The path Pando reports is the one relative to the knowledge base it imported,
// but a deployment may report it with a prefix in front (the corpus root, the
// mirror directory). Scanning from the right for the layout's own marker
// segment makes the parse independent of that prefix instead of tying the
// companion to one Pando release's spelling.
func parseCorpusPath(p string) (corpusRef, bool) {
	p = strings.TrimSpace(strings.ReplaceAll(p, "\\", "/"))
	if p == "" || !strings.HasSuffix(strings.ToLower(p), ".md") {
		return corpusRef{}, false
	}
	segments := strings.Split(strings.Trim(path.Clean(p), "/"), "/")
	// The marker needs a project before it and at least one segment after.
	for i := len(segments) - 2; i >= 1; i-- {
		switch segments[i] {
		case "items":
			// An item is exactly one file deep; anything else is not ours.
			if i+2 != len(segments) {
				continue
			}
			id := strings.TrimSuffix(segments[i+1], path.Ext(segments[i+1]))
			if id == "" {
				return corpusRef{}, false
			}
			return corpusRef{kind: "item", project: segments[i-1], rest: id}, true
		case "kb":
			return corpusRef{
				kind:    "page",
				project: segments[i-1],
				rest:    strings.Join(segments[i+1:], "/"),
			}, true
		}
	}
	return corpusRef{}, false
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
