package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Search by meaning.
//
// `search_items` and `search_kb` rank substrings: they answer "which item
// contains these words". This tool answers "which item is about this", which is
// the question an agent actually asks, and the one a substring index answers
// with nothing at all when the wording differs.
//
// The ranking lives behind the core contract (`search.semantic`, installed by
// the companion — internal/vault/semantic.go): this package only declares the
// schemas, validates the arguments and projects the answer. A session with no
// Pando backend gets `unavailable`, never an empty list, because "nothing
// matched" and "this session cannot rank by meaning" lead an agent to opposite
// conclusions.

const (
	// defaultSemanticHits is what the tool returns when the caller names no
	// limit. Semantic hits are candidates an agent then reads in full, so a
	// short list is worth more than a long one.
	defaultSemanticHits = 10
	// maxSemanticHits caps the list. Beyond this the tail is noise the agent
	// pays for; narrow the query instead.
	maxSemanticHits = 20
)

// SearchSemanticInput is a natural-language query over the backlog and the
// knowledge base.
type SearchSemanticInput struct {
	Query   string `json:"query" jsonschema:"What you are looking for, in words; for example how do we rotate refresh tokens"`
	Limit   int    `json:"limit,omitempty" jsonschema:"Hits to return, 1 to 20; default 10"`
	Project string `json:"project,omitempty" jsonschema:"Project key; omit for every mounted repository"`
	Kind    string `json:"kind,omitempty" jsonschema:"Restrict the answer to item, page or requirement; omit for all. item keeps a spec whole instead of resolving it to its requirements"`
}

// SemanticHits is the answer of search_semantic: candidates ranked by meaning,
// each one already resolved back to a live item or page of this workspace.
type SemanticHits struct {
	Hits []Hit `json:"hits"`
	// Engine names the backend that ranked the hits, so an agent reading a
	// transcript can tell which index produced an answer.
	Engine string `json:"engine" jsonschema:"Ranking backend that answered, for example pando"`
	// Degraded marks an answer the backend could only partly compute — a
	// corpus mid-import, an upstream that timed out. The hits are real; the
	// absence of a hit proves nothing.
	Degraded bool `json:"degraded,omitempty" jsonschema:"The backend answered from an incomplete index; treat a miss as inconclusive"`
}

// semanticEngine is the only backend the contract has today. The field exists
// because the REST answer carries one (docs/07), and an agent should never have
// to infer which index it is reading.
const semanticEngine = "pando"

// registerSearchTools declares the meaning-based half of the search surface.
func registerSearchTools(s *Server) {
	register(s, toolDef{
		Name:  "search_semantic",
		Title: "Search the backlog and knowledge base by meaning",
		Description: "Ranked-by-meaning search over backlog items, spec requirements and knowledge-base pages. " +
			"Use it for \"which stories or pages are about X\", where the wording of the question " +
			"is not the wording of the item; use search_items or search_kb when the exact words " +
			"appear in the text, and list_items when the question is a filter. " +
			"Scores come from the embedding backend and are comparable only with each other. " +
			"A hit inside a spec is the requirement it landed in: kind requirement, id the ref " +
			"(ACME-SP-0003.R2), with spec, anchor and the requirement rev. " +
			"Hits are candidates: read the winner with get_item or get_kb_page before answering.",
		Untrusted: true,
	}, searchSemantic)
}

// searchSemantic runs one meaning-based query and projects the candidates.
func searchSemantic(ctx context.Context, s *Server, in SearchSemanticInput) (SemanticHits, error) {
	query := strings.TrimSpace(in.Query)
	if query == "" {
		return SemanticHits{}, invalidField("query", "semantic search needs a query",
			"how do we rotate refresh tokens")
	}
	kind := strings.ToLower(strings.TrimSpace(in.Kind))
	if kind != "" && kind != "item" && kind != "page" && kind != "requirement" {
		return SemanticHits{}, invalidField("kind",
			"kind selects what to search: item, page, requirement, or nothing for all",
			[]string{"item", "page", "requirement"})
	}
	limit := in.Limit
	switch {
	case limit <= 0:
		limit = defaultSemanticHits
	case limit > maxSemanticHits:
		limit = maxSemanticHits
	}

	answer, err := dispatch[semanticAnswer](ctx, s, "search.semantic", map[string]any{
		"q": query, "limit": limit, "project": in.Project, "kind": kind,
	})
	if err != nil {
		return SemanticHits{}, semanticFailure(err)
	}

	hits := make([]Hit, 0, len(answer.Hits))
	for _, h := range answer.Hits {
		hit := Hit{
			Kind: h.Kind, ID: h.ID, Path: h.Path, Title: h.Title,
			Snippet: h.Snippet, Score: h.Score, Project: h.Project,
			Spec: h.Spec, Anchor: h.Anchor,
		}
		// A hit carries the token the next call needs: the rev of the item or
		// page it names, exactly as search_items and search_kb return one, so
		// acting on a candidate does not cost an extra read.
		switch hit.Kind {
		case "item":
			if hit.ID != "" {
				hit.Rev, hit.Status = s.itemRev(ctx, hit.ID)
			}
		case "page":
			if hit.Path != "" {
				hit.Rev = s.pageRev(ctx, hit.Path, hit.Project)
			}
		case "requirement":
			if hit.ID != "" {
				hit.Rev, hit.Status = s.requirementRev(ctx, hit.ID)
			}
		}
		hits = append(hits, hit)
	}
	engine := answer.Engine
	if engine == "" {
		engine = semanticEngine
	}
	return SemanticHits{Hits: hits, Engine: engine, Degraded: answer.Degraded}, nil
}

// semanticUnavailableRetry is what an agent does instead. It is spelled out in
// the error because an agent that reads only "unavailable" is one step away
// from reporting that nothing in the backlog matches.
const semanticUnavailableRetry = "This session cannot rank by meaning. Fall back to search_items " +
	"and search_kb with the words you expect to appear literally, or list_items with a filter. " +
	"Do not report that nothing matched: this query was never run."

// semanticFailure rewrites the refusal of a session without a backend. Every
// other failure is passed through with the code the rest of the product uses.
func semanticFailure(err error) error {
	var tool *toolError
	if !errors.As(err, &tool) || tool.Code != codeUnavailable {
		return err
	}
	return &toolError{
		Code:    codeUnavailable,
		Message: tool.Message,
		Retry:   semanticUnavailableRetry,
	}
}

// semanticAnswer decodes what "search.semantic" returns. The contract answers a
// bare array of hits today, while the REST surface answers
// `{"hits":…,"engine":…,"degraded":…}` (docs/07 §4.9); accepting both keeps the
// tool honest the day the core method starts reporting a degraded corpus,
// rather than silently dropping the flag.
type semanticAnswer struct {
	Hits []struct {
		Kind    string  `json:"kind"`
		ID      string  `json:"id"`
		Path    string  `json:"path"`
		Title   string  `json:"title"`
		Snippet string  `json:"snippet"`
		Score   float64 `json:"score"`
		Project string  `json:"project"`
		Spec    string  `json:"spec"`
		Anchor  string  `json:"anchor"`
	} `json:"hits"`
	Engine   string `json:"engine"`
	Degraded bool   `json:"degraded"`
}

// UnmarshalJSON accepts the array shape as well as the object one.
func (a *semanticAnswer) UnmarshalJSON(data []byte) error {
	type plain semanticAnswer
	if trimmed := strings.TrimLeft(string(data), " \t\r\n"); strings.HasPrefix(trimmed, "[") {
		var hits plain
		if err := json.Unmarshal(data, &hits.Hits); err != nil {
			return fmt.Errorf("decode the hits of search.semantic: %w", err)
		}
		*a = semanticAnswer(hits)
		return nil
	}
	var out plain
	if err := json.Unmarshal(data, &out); err != nil {
		return fmt.Errorf("decode the answer of search.semantic: %w", err)
	}
	*a = semanticAnswer(out)
	return nil
}
