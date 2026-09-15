package pando

import "time"

// KBSearchOptions are the optional parameters of kb_search_documents. The zero
// value asks for Pando's own defaults.
type KBSearchOptions struct {
	// Limit is the maximum number of hits. Values above Pando's cap of 20 are
	// clamped by the client rather than rejected by the server; 0 leaves
	// Pando's default of 5.
	Limit int
	// Tags filters by tag with Pando's fuzzy matching: a document matching any
	// of them is kept.
	Tags []string
	// ExcludeOutdated overrides Pando's default of true. Nil leaves it.
	ExcludeOutdated *bool
	// Scope restricts results to a memory scope prefix, for example "project/".
	Scope string
	// PathPrefix restricts both search legs to documents whose file path starts
	// with it, for example "corpus/git-in-track/". Pando applies it in SQL, so
	// it never under-returns when the best matches all live elsewhere.
	PathPrefix string
	// SortByDate sorts by updated_at descending instead of by relevance.
	SortByDate bool
}

// KBHit is one knowledge-base chunk returned by kb_search_documents.
type KBHit struct {
	// FilePath is the document path inside Pando's knowledge base, not a path
	// on disk.
	FilePath string
	// Chunk is the matching chunk of the document, not the whole document.
	Chunk string
	// Score is Pando's reciprocal-rank-fusion score. It is comparable only
	// with other KB hits from the same query; never with a CodeHit score.
	Score float64
	// Rank is the 1-based position in the result set.
	Rank int
	Tags []string
	// Metadata is the document's front matter as Pando stored it. Numbers
	// arrive as float64.
	Metadata  map[string]any
	CreatedAt time.Time
	UpdatedAt time.Time
	// Links and Backlinks count the wiki links the document declares and
	// receives. They are zero for documents outside Pando's wiki graph.
	Links     int
	Backlinks int
}

// CodeSearchOptions are the optional parameters of code_hybrid_search.
type CodeSearchOptions struct {
	// Limit is the page size. Values above Pando's cap of 50 are clamped; 0
	// leaves Pando's default of 20.
	Limit int
	// Offset skips ranked results; page forward with NextOffset.
	Offset int
	// Languages and SymbolTypes filter the candidate set.
	Languages   []string
	SymbolTypes []string
	// MinScore trims hits below a normalized relevance (0..1). 0 leaves
	// Pando's default trim at 15% of the top hit.
	MinScore float64
	// IncludeDocs includes Markdown and other documentation files. This client
	// always sends the field explicitly, because Pando's default is false and
	// a caller that wants documentation would otherwise silently get none.
	IncludeDocs bool
}

// CodeHit is one indexed symbol returned by code_hybrid_search.
type CodeHit struct {
	SymbolType string
	Name       string
	NamePath   string
	// FilePath is relative to the indexed project root.
	FilePath  string
	StartLine int
	Signature string
	Snippet   string
	Kind      string
	// Score is normalized to 0..1 against the top hit of this query.
	Score float64
	Rank  int
}

// Project is one indexed code project returned by code_list_projects.
type Project struct {
	// ProjectID is Pando's sanitized identifier, the value SearchCode takes.
	ProjectID string
	Name      string
	RootPath  string
	// IndexingStatus is Pando's own vocabulary, for example "completed".
	IndexingStatus string
	// LanguageStats counts indexed files per language.
	LanguageStats map[string]int
	CreatedAt     time.Time
	UpdatedAt     time.Time
	LastIndexedAt time.Time
}

// ReindexStats is the outcome of a REST knowledge-base reindex.
type ReindexStats struct {
	Scanned   int
	Added     int
	Updated   int
	Unchanged int
	Deleted   int
	// LinksIndexed counts the wiki links found in the documents this run added
	// or updated, not the size of the graph.
	LinksIndexed int
}
