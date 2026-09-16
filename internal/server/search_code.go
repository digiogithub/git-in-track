package server

import (
	"context"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/digiogithub/git-in-track/internal/core"
	"github.com/digiogithub/git-in-track/internal/pando"
	"github.com/digiogithub/git-in-track/internal/vault"
)

// The code half of the semantic search (GIT-US-0098).
//
// Pando keeps two indexations of this repository and they divide the work
// cleanly: the knowledge-base one walks the documentation directory with no
// exclusions, so it reaches the backlog under `docs/.pmngr/`, and the code one
// walks the repository root but skips every dot-directory, so it reaches the
// source, the README and the Markdown outside the knowledge base and *cannot*
// see the backlog. The one thing they both hold is `docs/*.md`, which is why
// the merge below exists.

// scoredHit is one resolved candidate carrying the score of its own leg,
// rescaled so the two legs can be compared at all.
//
// Raw scores cannot: a knowledge-base hit carries a reciprocal-rank-fusion
// score around 0.016 while a code hit carries a relevance already normalized
// against the top hit of its own query. So each leg is divided by its own best
// score, which turns both into "how good is this, for this query, on its own
// index" — the only comparison the two scales support. The hit keeps its own
// raw Score; only the ordering and the "better score" of a merge use rel.
type scoredHit struct {
	hit core.SearchHit
	// repo is the mounted repository the document was resolved in. It scopes
	// the merge key, because two clones can hold the same relative path.
	repo string
	rel  float64
}

// relative rescales one leg's score against that leg's best. A top score of
// zero — every candidate scored nothing — leaves every hit at zero, which
// leaves them in the order Pando ranked them.
func relative(score, top float64) float64 {
	if top <= 0 {
		return 0
	}
	return score / top
}

// topKBScore is the best score of a knowledge-base result set.
func topKBScore(hits []pando.KBHit) float64 {
	top := 0.0
	for _, h := range hits {
		if h.Score > top {
			top = h.Score
		}
	}
	return top
}

// topCodeScore is the best score of a code result set.
func topCodeScore(hits []pando.CodeHit) float64 {
	top := 0.0
	for _, h := range hits {
		if h.Score > top {
			top = h.Score
		}
	}
	return top
}

// searchCode runs the code leg over every registered code project and resolves
// what comes back against the repository the project was registered from.
//
// It never fails the query: a project Pando has not indexed yet, or a code
// index that is simply off, must cost the caller the code half and nothing
// else. The count it returns is how many candidates named a file that is no
// longer there.
func (p *pandoSearcher) searchCode(ctx context.Context, q vault.SemanticQuery) (hits []scoredHit, dropped int) {
	if p == nil || p.client == nil || len(p.projects) == 0 {
		return nil, 0
	}
	// A query scoped to items or pages by kind still runs: a `docs/` file the
	// code index holds resolves to a page, and dropping the leg on q.Kind
	// would lose exactly the hits the merge is for. Scoping happens on the
	// resolved hit, as it does for the knowledge-base leg.
	var (
		mu sync.Mutex
		wg sync.WaitGroup
	)
	for _, project := range p.projects {
		wg.Add(1)
		go func(project codeProject) {
			defer wg.Done()
			found, err := p.client.SearchCode(ctx, project.id, q.Q, pando.CodeSearchOptions{
				Limit: overFetch(q.Limit),
				// Markdown is the point: `docs/*.md`, the README and the
				// changelog are what a question about this repository is most
				// often answered from, and Pando excludes documentation by
				// default.
				IncludeDocs: true,
			})
			if err != nil {
				// Not a failure of the search: the knowledge-base leg carries
				// the answer and the settings card carries the reason.
				p.log.Debug("code search leg failed", "project", project.id, "error", err)
				return
			}
			top := topCodeScore(found)
			local := make([]scoredHit, 0, len(found))
			misses := 0
			for _, h := range found {
				hit, ok := p.resolveCode(project, h, q)
				if !ok {
					misses++
					continue
				}
				local = append(local, scoredHit{hit: hit, repo: project.repo, rel: relative(h.Score, top)})
			}
			mu.Lock()
			hits = append(hits, local...)
			dropped += misses
			mu.Unlock()
		}(project)
	}
	wg.Wait()
	return hits, dropped
}

// resolveCode maps one code candidate back onto this workspace: a page when the
// file is one of the repository's knowledge-base documents, a plain file
// otherwise. A candidate whose file is gone is dropped, the same way a stale
// knowledge-base candidate is.
//
// A code hit carries a symbol name — a heading, for Markdown — and a path, and
// no front matter at all, so a file hit takes its title from the symbol. That
// is exactly why the merge fills a hit's empty fields from the other side.
func (p *pandoSearcher) resolveCode(project codeProject, c pando.CodeHit, q vault.SemanticQuery) (core.SearchHit, bool) {
	rel := strings.TrimSpace(strings.ReplaceAll(c.FilePath, "\\", "/"))
	if rel == "" {
		return core.SearchHit{}, false
	}
	rel = path.Clean(rel)
	if path.IsAbs(rel) {
		under, ok := underTree(project.root, rel)
		if !ok {
			return core.SearchHit{}, false
		}
		rel = under
	}
	hit := core.SearchHit{
		Kind:    "file",
		Path:    rel,
		Title:   c.Name,
		Score:   c.Score,
		Snippet: codeSnippet(c),
		Source:  core.SearchSourcePando,
		Index:   core.SearchIndexCode,
	}
	if m, ok := p.repos.lookup(project.repo); ok && m.ready() {
		if page, found := m.vlt.Page(rel); found {
			hit.Kind, hit.Path, hit.Project = "page", page.Path, page.Project
			if page.Title != "" {
				hit.Title = page.Title
			}
			if hit.Project == "" {
				hit.Project = core.ProjectKey(m.id)
			}
		} else if info, err := os.Stat(filepath.Join(m.path, filepath.FromSlash(rel))); err != nil || info.IsDir() {
			return core.SearchHit{}, false
		}
	}
	if q.Kind != "" && q.Kind != hit.Kind {
		return core.SearchHit{}, false
	}
	if q.Project != "" && q.Project != string(hit.Project) {
		return core.SearchHit{}, false
	}
	return hit, true
}

// codeSnippet renders a code candidate as the one-line excerpt every other row
// carries: the matched fragment, or the signature when Pando sent none.
func codeSnippet(c pando.CodeHit) string {
	for _, text := range []string{c.Snippet, c.Signature, c.NamePath} {
		if s := chunkSnippet(text); s != "" {
			return s
		}
	}
	return ""
}

// -------------------------------------------------------------- the merge ---

// semanticMerge deduplicates the two legs at presentation.
//
// `docs/*.md` is a knowledge-base document through Pando's KBPath *and*
// indexable Markdown for the code indexer, so one file arrives from both sides
// with two scores on two scales. Filtering the documentation directory out of
// the code side is not an option — Pando's code exclusions are hardcoded — and
// showing the file twice is worse, so the merge happens here: one row per
// resolved file, carrying the better of the two relative scores, and every
// field the winner lacks filled in from the loser.
type semanticMerge struct {
	at  map[string]int
	out []scoredHit
}

func newSemanticMerge(size int) *semanticMerge {
	return &semanticMerge{at: make(map[string]int, size), out: make([]scoredHit, 0, size)}
}

// mergeKey identifies the document a hit is about. An item is keyed by its id,
// which is unique across the workspace; everything else by the repository and
// the resolved file path, which is the only identity a code hit and a
// knowledge-base hit of the same file share.
func mergeKey(h scoredHit) string {
	if h.hit.ID != "" {
		return "item\x00" + string(h.hit.ID)
	}
	return "path\x00" + h.repo + "\x00" + h.hit.Path
}

// add folds one resolved candidate in.
func (m *semanticMerge) add(h scoredHit) {
	key := mergeKey(h)
	i, seen := m.at[key]
	if !seen {
		m.at[key] = len(m.out)
		m.out = append(m.out, h)
		return
	}
	// A document is chunked, and an item's comment thread is a directory of
	// files, so one item can come back many times from the same leg. The
	// best-ranked fragment wins; a further comment that matched is counted
	// rather than dropped, so a thread that answers the query in five places
	// says so instead of filling the list.
	if h.hit.Match == core.SearchMatchComment && m.out[i].hit.Index == h.hit.Index {
		m.out[i].hit.MoreMatches++
		return
	}
	kept := m.out[i]
	if h.rel > kept.rel {
		fillFrom(&h.hit, kept.hit)
		h.repo = firstNonEmpty(h.repo, kept.repo)
		m.out[i] = h
		return
	}
	fillFrom(&m.out[i].hit, h.hit)
}

// fillFrom completes the winning row with what the losing one knows.
//
// This is the half of the merge that has to survive one side having no front
// matter: a code hit knows a path and a symbol name, a knowledge-base hit knows
// the title, the id and the project. Whichever won the score, the row a user
// sees carries both.
func fillFrom(dst *core.SearchHit, src core.SearchHit) {
	if dst.Title == "" {
		dst.Title = src.Title
	}
	if dst.ID == "" {
		dst.ID = src.ID
	}
	if dst.Project == "" {
		dst.Project = src.Project
	}
	if dst.Snippet == "" {
		dst.Snippet = src.Snippet
	}
	// "file" is what a resolver answers when it could not say more, so a side
	// that recognized the document as a page or an item names the kind.
	if dst.Kind == "file" && src.Kind != "" && src.Kind != "file" {
		dst.Kind = src.Kind
		if src.Path != "" {
			dst.Path = src.Path
		}
	}
	if src.MoreMatches > dst.MoreMatches {
		dst.MoreMatches = src.MoreMatches
	}
}

// hits returns the merged rows, best first, cut to limit. The order is by the
// rescaled score, so a code hit and a knowledge-base hit are ranked against
// each other on the only footing they share; ties keep the order they were
// added in, which puts the knowledge base first.
func (m *semanticMerge) hits(limit int) []core.SearchHit {
	sort.SliceStable(m.out, func(i, j int) bool { return m.out[i].rel > m.out[j].rel })
	if limit > 0 && len(m.out) > limit {
		m.out = m.out[:limit]
	}
	out := make([]core.SearchHit, 0, len(m.out))
	for _, h := range m.out {
		out = append(out, h.hit)
	}
	return out
}
