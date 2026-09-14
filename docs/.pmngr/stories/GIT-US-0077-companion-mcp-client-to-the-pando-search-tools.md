---
id: GIT-US-0077
type: story
title: Companion MCP client to the Pando search tools
status: backlog
priority: high
parent: GIT-EP-0019
milestone: GIT-M-0013
author: mcp
labels: [server]
estimate: 5
created: 2026-09-13T13:14:00Z
updated: 2026-09-13T21:18:40Z
---

## Description

As the companion, I want a typed client that calls Pando's search tools over MCP, so that semantic search has a single, testable seam instead of ad-hoc HTTP scattered through the server.

**Buildable today, verified.** Pando exposes no REST search API, so this goes over MCP — and the go-sdk v1.4.1 streamable client has been verified against Pando's hand-rolled `/mcp` endpoint, 405s on GET and DELETE included. Add `internal/pando` with a `Client` built on `github.com/modelcontextprotocol/go-sdk/mcp` (already a direct dependency, `go.mod:12`, already used server-side in `internal/mcp/transport.go:41`) speaking streamable HTTP to Pando's `/mcp`. Expose `SearchKB(ctx, query string, opts)`, `SearchCode(ctx, projectID, query string, opts)`, `ListProjects(ctx)` and `Health(ctx)`, each returning parsed Go structs rather than raw tool content.

**This package is the transport seam for the whole epic.** MCP today, plain REST later when Pando grows a search route (PANDO-EP-0005) — the switch must cost nothing above this package boundary, so no caller may see an MCP type.

Add `config.Search{Pando struct{URL, ProjectID string}}` to `internal/config/config.go`, validated like the rest, defaulting `project_id` to the sanitised repository path Pando itself would derive (`/www/git-in-track` becomes `www_git-in-track`). The client owns a bounded session: a per-call timeout, one lazily established session reused across calls and reconnected on failure (Pando's session map has no eviction, so reuse matters), and a circuit that reports unhealthy rather than hanging the HTTP handler that called it.

**Refuse any URL that is not loopback — non-overridable in v1.** Pando's MCP HTTP transport is not merely unauthenticated: it serves CORS `*` with `SetGlobalAutoApprove(true)`. There is no operator opt-in flag for a non-loopback URL in this version; a remote Pando needs a design that does not exist yet. And the refusal must be documented honestly: **loopback binding is not a boundary against the user's own browser** while that CORS policy is `*` — any web page the user visits can reach `:9777`. PANDO-EP-0006 is the fix.

## Acceptance Criteria

- [ ] `internal/pando.Client` connects over streamable HTTP using the MCP Go SDK and reuses one session across calls.
- [ ] No MCP type appears in the package's exported surface, so a later REST implementation is a drop-in.
- [ ] `SearchKB` parses `kb_search_documents` results into `{filePath, chunk, score, rank, tags, updated, metadata}`.
- [ ] `SearchCode` parses `code_hybrid_search` results; `ListProjects` parses `code_list_projects`.
- [ ] Every call honours a configurable timeout and returns a typed error on timeout, transport failure or a tool-level error.
- [ ] `Health` reports reachable or not without blocking longer than its timeout, and a dropped session reconnects on the next call.
- [ ] `config.Search.Pando{url, project_id}` parses, validates, and **refuses a non-loopback URL with no override**; the refusal is covered by a test.
- [ ] The doc comment and `docs/07-cli-and-api.md` state that loopback is not a boundary against the browser while Pando's MCP CORS policy is `*` with global auto-approve.
- [ ] `internal/pando` imports nothing from `internal/core` and is never imported by it.
- [ ] `go test -race ./internal/pando/...` covers parsing, timeout, reconnect and the non-loopback refusal against an in-process MCP stub server.

## Notes

Transport compatibility verified against go-sdk v1.4.1: the streamable client works with Pando's `/mcp` as it stands, with no shim.

Tool contracts: `kb_search_documents(query*, limit ≤20 default 5, tags[] fuzzy, sort_by_date, exclude_outdated default true, scope)` — Pando `internal/llm/tools/remembrances_kb.go:227-259`, limit clamped at `:274-279`, result shape `:296-316` (it does include `metadata`, so a fidelity fix upstream would pay off with no client change); `code_hybrid_search(project_id*, query*, limit ≤50 default 20, offset, languages[], symbol_types[], min_score, include_docs **default false — Markdown excluded**, group_by_file)` — `remembrances_code.go:300`; `code_list_projects` `:1142`; `project_id` sanitisation `:1523-1543`.

Prefer calling `kb_search_documents` and `code_hybrid_search` separately over `hybrid_search_remembrances`, whose merge sorts incommensurable scores (KB RRF around 0.016 against boosted code scores around 1.0, `internal/rag/hybrid.go:118-121`).

This package must stay native-only: `internal/core` compiles to WASM and cannot hold `net/http` (AGENTS.md, `internal/core/doc.go:11-12`). Pando is per-repository and holds an `ipc.lock`, so assume exactly one instance per repository — which matches the one-`agui-serve`-per-repository deployment in GIT-EP-0018.

**Pando dependencies (plain ids):** PANDO-EP-0006 (authenticate the MCP HTTP transport) — makes a non-loopback URL conceivable and closes the browser-reachability hole. PANDO-EP-0005 (REST search surface) — lets this client drop MCP for plain HTTP behind the same interface. Neither blocks: the story is buildable today.
