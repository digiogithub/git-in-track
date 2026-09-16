package core

// This file holds the search contract of docs/02 section 8. It is deliberately
// tiny and dependency-free: `internal/core` compiles to WebAssembly (ADR-003),
// so the interface may name nothing that a browser build cannot link.

// The backends a [SearchHit] can come from. They are the values of
// [SearchHit.Source] and of the `features.search` capability, and they are
// declared here rather than in the companion so that the browser build, which
// only ever produces SourceCore, spells the value the same way.
const (
	// SearchSourceCore is the substring index of [Index.Search]. It is the only
	// backend a browser-only session has, and the fallback of every other one.
	SearchSourceCore = "core"
	// SearchSourcePando is the optional native accelerator: semantic
	// candidates from a Pando corpus, resolved back into this index before
	// anything is shown (GIT-US-0082).
	SearchSourcePando = "pando"
)

// The indexes a [SearchSourcePando] hit can have come from. Pando keeps two
// (GIT-US-0098): the knowledge-base indexation of the documentation directory,
// which reaches the backlog under `.pmngr/`, and the code indexation of the
// repository root, which cannot see a dot-directory but does cover the source
// and the Markdown outside the knowledge base. They are the values of
// [SearchHit.Index], and a client labels a row with them so a code hit is never
// mistaken for a backlog item.
const (
	// SearchIndexKB is Pando's knowledge-base indexation (`kb_search_documents`).
	SearchIndexKB = "kb"
	// SearchIndexCode is Pando's code indexation (`code_hybrid_search`).
	SearchIndexCode = "code"
)

// Searcher is the search contract: one query, one bounded list of hits, ranked
// by the backend's own scale.
//
// *[Index] satisfies it, and so does the Pando-backed searcher the companion
// builds in internal/server. Scores are comparable only within one backend:
// a Pando fusion score and a core field-weight score live in different spaces
// and must never be sorted against each other (docs/02 section 8).
type Searcher interface {
	// Search returns at most limit hits for q. A limit of zero means the
	// backend's own default. An empty query returns no hits, never an error:
	// search is a read that always answers.
	Search(q string, limit int) []SearchHit
}

// The index is the reference implementation of the contract. This assertion is
// the whole of GIT-T-0159's "with no behavior change": if Index.Search ever
// drifts from the signature, the build says so here.
var _ Searcher = (*Index)(nil)
