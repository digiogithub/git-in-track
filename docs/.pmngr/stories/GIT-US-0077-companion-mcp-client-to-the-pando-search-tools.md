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
updated: 2026-09-13T13:14:00Z
---

## Description

As the companion, I want a typed client that calls Pando's search tools over MCP, so that semantic search has a single, testable seam instead of ad-hoc HTTP scattered through the server.

Pando exposes **no REST search API** — KB, code and memory search are reachable only through MCP tools or a full agent run. Add `internal/pando` with a `Client` built on `github.com/modelcontextprotocol/go-sdk/mcp` (already a direct dependency, `go.mod:12`) speaking streamable HTTP to `pando mcp-server` at its `/mcp` endpoint. Expose `SearchKB(ctx, query string, opts)`, `SearchCode(ctx, projectID, query string, opts)`, `ListProjects(ctx)` and `Health(ctx)`, each returning parsed Go structs rather than raw tool content.

Add `config.Search{Pando struct{URL, ProjectID string}}` to `internal/config/config.go`, validated like the rest, defaulting `project_id` to the sanitised repository path Pando itself would derive (`/www/git-in-track` becomes `www_git-in-track`). The client owns a bounded session: a per-call timeout, one lazily established session reconnected on failure, and a circuit that reports unhealthy rather than hanging the HTTP handler that called it. Since Pando's MCP HTTP transport has no authentication, refuse any URL that is not loopback unless the operator opts in explicitly.

## Acceptance Criteria

- [ ] `internal/pando.Client` connects over streamable HTTP using the MCP Go SDK and reuses one session across calls.
- [ ] `SearchKB` parses `kb_search_documents` results into `{filePath, chunk, score, rank, tags, updated, metadata}`.
- [ ] `SearchCode` parses `code_hybrid_search` results; `ListProjects` parses `code_list_projects`.
- [ ] Every call honours a configurable timeout and returns a typed error on timeout, transport failure or a tool-level error.
- [ ] `Health` reports reachable or not without blocking longer than its timeout, and a dropped session reconnects on the next call.
- [ ] `config.Search.Pando{url, project_id}` parses, validates, and rejects a non-loopback URL unless explicitly allowed.
- [ ] `internal/pando` imports nothing from `internal/core` and is never imported by it.
- [ ] `go test -race ./internal/pando/...` covers parsing, timeout, reconnect and the non-loopback refusal against an in-process MCP stub server.

## Notes

Tool contracts: `kb_search_documents(query*, limit ≤20 default 5, tags[] fuzzy, sort_by_date, exclude_outdated default true, scope)` — Pando `internal/llm/tools/remembrances_kb.go:226`, result shape `:304-315`; `code_hybrid_search(project_id*, query*, limit ≤50 default 20, offset, languages[], symbol_types[], min_score, include_docs **default false — Markdown excluded**, group_by_file)` — `remembrances_code.go:300`; `code_list_projects` `:1142`. `project_id` sanitisation is `:1523-1543`.

Prefer calling `kb_search_documents` and `code_hybrid_search` separately over `hybrid_search_remembrances`, whose merge sorts incommensurable scores (KB RRF around 0.016 against boosted code scores around 1.0, `internal/rag/hybrid.go:119-121`).

This package must stay native-only: `internal/core` compiles to WASM and cannot hold `net/http` (AGENTS.md, `internal/core/doc.go:11-12`). Pando is per-project and holds an `ipc.lock`, so assume exactly one instance per repository.
