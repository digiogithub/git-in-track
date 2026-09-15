// Package pando is the companion's typed client for Pando's semantic search
// surface: the knowledge base, the code index, and the reindex that keeps the
// exported corpus in step.
//
// It is the transport seam for the whole semantic-search epic. Everything above
// it — the HTTP handlers, the MCP tools, the web client — sees Client, KBHit,
// CodeHit, Project, ReindexStats and the sentinels in errors.go, and nothing
// else. No MCP type appears in the exported surface, so when Pando grows the
// REST search routes it is missing today the switch costs this package and
// nothing above it.
//
// Like internal/youtrack and internal/gitops, this is a native-only package: it
// speaks HTTP and therefore cannot live in internal/core, which is also
// compiled to WebAssembly and forbids net/http (see internal/core/doc.go). It
// imports nothing from internal/core and internal/core must never import it.
//
// # Why MCP, and not REST
//
// Pando has no REST search API. Its REST surface (`pando serve`) exposes
// document CRUD and the knowledge-base reindex, but the searches themselves —
// kb_search_documents, code_hybrid_search, code_list_projects — exist only as
// MCP tools on `pando mcp-server`. So the client speaks streamable HTTP MCP
// with github.com/modelcontextprotocol/go-sdk, which is already a direct
// dependency and is already how the companion serves its own MCP endpoint
// (internal/mcp/transport.go). The go-sdk streamable client is verified against
// Pando's hand-rolled /mcp endpoint, 405s on GET and DELETE included, so
// DisableStandaloneSSE is set and no shim is needed.
//
// The one exception is ReindexKB, which is REST because Pando offers it nowhere
// else. It is configured separately (RESTURL, RESTToken) and returns
// ErrNotConfigured when it is not configured, because the REST surface needs a
// different Pando process than the MCP one and may simply not be running.
//
// # Why the two searches are called separately
//
// Pando also exposes hybrid_search_remembrances, which searches the knowledge
// base and the code index in one call. This client does not use it. Its merge
// sorts scores that are not comparable: knowledge-base hits carry a reciprocal
// rank fusion score around 0.016 while code hits carry a boosted score around
// 1.0 (Pando internal/rag/hybrid.go), so the code side always wins and the
// knowledge-base hits fall off the end of the list. Calling the two tools
// separately and merging in the caller — which knows what it is ranking for —
// is the only way to get a useful ordering. Do not "simplify" this back into
// one call without fixing the scoring upstream first.
//
// # Why loopback, and what loopback does not protect
//
// New refuses a Pando URL whose host is not a loopback address unless
// Options.AllowRemote is set, and nothing else in this package can override
// that refusal: it is not a policy the rest of the companion gets a vote on.
//
// The reason is what Pando's MCP HTTP transport is. It now requires a bearer
// token, which is why Options.Token exists — but it still serves CORS `*` and
// runs with global auto-approve, and it exposes far more than search: the tool
// set includes file writes, shell execution and agent spawning. A companion
// that could be pointed at a remote Pando would be a remote-code-execution
// gadget wearing a search feature's clothes. Remote Pando needs a design that
// does not exist yet; until it does, the answer is no.
//
// And the honest caveat, which belongs in the operator's head and not only in
// this comment: **loopback binding is not a boundary against the user's own
// browser.** While Pando's MCP CORS policy is `*`, any web page the user
// visits can reach 127.0.0.1:9777 from their browser, with no companion
// involved at all. Binding to loopback keeps the *network* out; it does not
// keep a hostile page out. The fix is Pando's, not this package's.
//
// # Result parsing
//
// Pando renders structured tool results through its own formatter, which
// prefers TOON (Token-Oriented Object Notation) over JSON for token
// efficiency. A tool result therefore arrives as a text content block in a
// format that is neither JSON nor YAML, and toon.go decodes the subset of TOON
// v4.1 that Pando's encoder emits. code_hybrid_search is the exception: its
// text content is compact human-readable lines and the machine-readable result
// travels in structuredContent.metadata as JSON, which is what SearchCode
// parses. Both paths tolerate unknown fields, because Pando adds them without
// warning.
//
// # Lifecycle
//
// One MCP session is established lazily and shared by every caller. Reuse is
// not just an optimisation: Pando's session map has no eviction, so a client
// that initialized per call would leak a session per call for the lifetime of
// the process. A session that has died — Pando restarted, the session id is
// gone — is rebuilt once, transparently, on the next call. Every call carries
// its own deadline (Options.Timeout, default DefaultTimeout) because every
// caller is an HTTP handler that must not be held open by a Pando that is busy
// embedding a corpus.
//
// # Errors
//
// Failures are reported as, or wrapped around, the sentinels in errors.go so
// the server can map them onto status codes without matching on strings:
// ErrNotConfigured (the feature is off), ErrInvalidOptions and ErrRemoteRefused
// (New rejected the configuration), ErrUnreachable (Pando is not answering),
// ErrUnauthorized (the token was rejected), ErrTimeout (the deadline expired,
// which is deliberately not the same as unreachable), ErrToolFailed (Pando ran
// the tool and the tool failed, which is deliberately not the same as an empty
// result set) and ErrReindexRunning (a reindex is already walking the corpus).
package pando
