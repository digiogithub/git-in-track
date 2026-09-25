package vault

import (
	"context"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/digiogithub/git-in-track/internal/core"
)

// Duplicate detection on create (GIT-US-0111). When a requirement is created,
// the host-installed semantic searcher is asked for requirement blocks that
// read like it, and they are returned next to the write as `similar[]`. The
// suggestions are advisory: the requirement is already written, nothing is
// refused, and a session with no backend, a failing backend or a slow one
// answers an empty list, never an error. This file holds no network code; the
// ranking stays behind the SemanticSearcher seam (semantic.go).

// The bounds of the duplicate check.
const (
	// SimilarRequirementsLimit caps similar[].
	SimilarRequirementsLimit = 3
	// SimilarRequirementsTimeout bounds how long a create waits for the
	// backend. On timeout the create answers with no suggestions.
	SimilarRequirementsTimeout = 2 * time.Second
	// SimilarRequirementsRelativeScore is the score threshold. Semantic scores
	// are on the backend's own scale (docs/02 section 8), so the cut is
	// relative: a candidate is kept when its score is positive and at least
	// this fraction of the best candidate's.
	SimilarRequirementsRelativeScore = 0.5
)

// SimilarRequirement is one entry of similar[]: an existing requirement block
// the backend ranks near a new one.
type SimilarRequirement struct {
	Ref     string  `json:"ref"`
	Title   string  `json:"title"`
	Spec    string  `json:"spec"`
	Status  string  `json:"status,omitempty"`
	Project string  `json:"project,omitempty"`
	VaultID string  `json:"vaultId,omitempty"`
	Anchor  string  `json:"anchor,omitempty"`
	Score   float64 `json:"score"`
}

// SimilarRequirements asks the semantic backend for requirement blocks close to
// view — the query is its title and statement — within view's project. It
// never fails: without a backend, on an error or past
// SimilarRequirementsTimeout it returns an empty, non-nil slice. The
// requirement itself is never among the answers.
func (w *Workspace) SimilarRequirements(ctx context.Context, view core.RequirementView) []SimilarRequirement {
	out := []SimilarRequirement{}
	backend := w.semanticSearcher()
	if backend == nil {
		return out
	}
	statement := view.Statement
	if strings.TrimSpace(statement) == "" {
		statement = view.Text
	}
	q := strings.TrimSpace(strings.TrimSpace(view.Title) + "\n" + strings.TrimSpace(statement))
	if q == "" {
		return out
	}
	project := view.Project
	if project == "" {
		if key, _, _, err := core.ParseItemID(string(view.Spec)); err == nil {
			project = key
		}
	}

	ctx, cancel := context.WithTimeout(ctx, SimilarRequirementsTimeout)
	defer cancel()
	type answer struct {
		hits []core.SearchHit
		err  error
	}
	// The call runs aside so that a backend which ignores its context still
	// cannot hold the create past the timeout.
	done := make(chan answer, 1)
	go func() {
		hits, err := backend.SearchSemantic(ctx, SemanticQuery{
			Q: q, Project: string(project), Kind: core.SearchKindRequirement,
			// Room for the requirement itself and the hits the filter drops.
			Limit: SimilarRequirementsLimit * 3,
		})
		done <- answer{hits, err}
	}()
	var got answer
	select {
	case got = <-done:
	case <-ctx.Done():
		return out
	}
	if got.err != nil {
		return out
	}

	self := view.Ref.String()
	seen := map[string]bool{}
	for _, h := range got.hits {
		ref := string(h.ID)
		if h.Kind != core.SearchKindRequirement || ref == "" || ref == self || seen[ref] || h.Score <= 0 {
			continue
		}
		seen[ref] = true
		s := SimilarRequirement{
			Ref: ref, Title: h.Title, Spec: string(h.Spec), Status: string(h.Status),
			Project: string(h.Project), Anchor: h.Anchor, Score: math.Round(h.Score*1000) / 1000,
		}
		if m, ok := w.MountForProject(h.Project); ok {
			s.VaultID = m.ID
		}
		out = append(out, s)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	if len(out) > 0 {
		floor := out[0].Score * SimilarRequirementsRelativeScore
		kept := out[:0]
		for _, s := range out {
			if s.Score >= floor {
				kept = append(kept, s)
			}
		}
		out = kept
	}
	if len(out) > SimilarRequirementsLimit {
		out = out[:SimilarRequirementsLimit]
	}
	return out
}

// WithSimilarRequirements adds similar[] to the answer of a requirement.create:
// a map carrying the created core.RequirementView under "requirement". Any
// other value is returned unchanged.
func (w *Workspace) WithSimilarRequirements(ctx context.Context, result any) any {
	m, ok := result.(map[string]any)
	if !ok {
		return result
	}
	view, ok := m["requirement"].(core.RequirementView)
	if !ok {
		return result
	}
	m["similar"] = w.SimilarRequirements(ctx, view)
	return m
}
