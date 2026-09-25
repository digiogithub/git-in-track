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

// ImpactOptions are the optional parameters of code_impact_analysis. The zero
// value asks for Pando's own defaults.
type ImpactOptions struct {
	// Depth is the maximum transitive caller depth; 0 leaves Pando's default
	// of 2.
	Depth int
	// Limit caps the callers returned per analyzed symbol; 0 leaves Pando's
	// default of 50.
	Limit int
}

// ImpactResult is the outcome of ImpactAnalysis over one or more symbols.
type ImpactResult struct {
	// Callers are the symbols that call an analyzed symbol directly or
	// transitively, grouped by analyzed symbol in the order they were asked
	// for and, within one symbol, in Pando's order. A caller that depends on
	// two analyzed symbols appears once for each.
	Callers []ImpactCaller
	// Truncated reports that Pando cut at least one symbol's caller list at
	// Limit, so the impact set may be larger than Callers.
	Truncated bool
}

// ImpactCaller is one symbol that depends on an analyzed symbol.
type ImpactCaller struct {
	// Symbol is the analyzed symbol this caller depends on, as it was passed
	// to ImpactAnalysis.
	Symbol     string
	Name       string
	NamePath   string
	SymbolType string
	// FilePath is relative to the indexed project root.
	FilePath string
	// StartLine and EndLine are the caller's line range. Pando reports only
	// the start line today, so EndLine is 0 ("unknown") until it adds one.
	StartLine int
	EndLine   int
	// Depth is the call distance: 1 for a direct caller.
	Depth int
}

// FindSymbolOptions are the optional parameters of code_find_symbol.
type FindSymbolOptions struct {
	// RelativePath restricts the search to one file or directory of the
	// project.
	RelativePath string
	// SymbolTypes and Languages filter the candidate set.
	SymbolTypes []string
	Languages   []string
	// Substring enables partial name matching.
	Substring bool
	// Limit is the page size; 0 leaves Pando's default of 50.
	Limit int
	// Offset skips ranked results.
	Offset int
}

// Symbol is one symbol definition returned by code_find_symbol.
type Symbol struct {
	Name       string
	NamePath   string
	SymbolType string
	// FilePath is relative to the indexed project root.
	FilePath string
	// StartLine and EndLine are the definition's line range. EndLine is 0
	// ("unknown") while Pando reports only the start line.
	StartLine int
	EndLine   int
	Signature string
	// Rank is the 1-based position in the whole result set, Offset included.
	Rank int
}

// RelatedFilesOptions are the optional parameters of code_related_files.
type RelatedFilesOptions struct {
	// Limit caps the files returned; 0 leaves Pando's default of 20.
	Limit int
}

// RelatedFilesResult is the outcome of RelatedFiles.
type RelatedFilesResult struct {
	// Files are ranked by Score, highest first, as Pando returned them.
	Files []RelatedFile
	// Truncated reports that Pando cut the list at Limit.
	Truncated bool
}

// RelatedFile is one file coupled to the queried file in Pando's code graph.
type RelatedFile struct {
	// FilePath is relative to the indexed project root.
	FilePath string
	// Score blends import edges (weight 1.0) and call coupling (weight 0.8).
	// It is comparable only with other files of the same query.
	Score float64
	// Reasons names the kinds of coupling found, for example "imports" or
	// "calls".
	Reasons []string
}
