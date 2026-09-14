---
id: GIT-EP-0019
type: epic
title: Semantic search with Pando
status: backlog
priority: medium
milestone: GIT-M-0013
author: mcp
labels: [server, core, web, mcp, performance]
created: 2026-09-13T13:08:37Z
updated: 2026-09-13T21:16:50Z
---

## Description

git-in-track's search is a weighted substring matcher (`core.Index.Search`). Pando has hybrid BM25 + embedding search over a KB corpus and a code index, reachable only through MCP. This epic wires the two: the companion exports every item and KB page as a Markdown document with front matter into a corpus directory (`.pando-kb/` in the cache dir, one file per item, id and labels as tags, wikilinks preserved); Pando indexes it with `[Remembrances] KBPath` + **`KBAutoImport = true`, `KBWatch = false`** — the watcher is off, and the companion drives re-sync. The companion then queries `kb_search_documents` (and `code_hybrid_search` for the source tree) over Pando's MCP HTTP transport and offers the results as an optional search backend, reported through the existing `fullTextSearch` capability, in the search UI and to the agent as a tool.

**Semantic search returns candidates, not answers.** Pando hits are resolved back to item ids and the companion re-reads the authoritative fields — status, type, milestone, parent, labels — from its own index. No structured filtering is asked of Pando, and nothing rendered to a user comes from Pando's copy of the metadata.

## Acceptance Criteria

- [ ] Exporter: full export on start, incremental on `item.changed` / `file.changed` events, deletions pruned; never points Pando at the repository root.
- [ ] `KBWatch` is off; re-sync is companion-driven — today by waiting for Pando's next auto-import pass, later by calling Pando's REST reindex route when PANDO-EP-0005 lands.
- [ ] Companion MCP client to Pando (streamable HTTP, loopback), health and timeouts.
- [ ] `core/search` contract gains a native `pando` implementation; capability `fullTextSearch: 'core' | 'bleve' | 'pando'`; fallback to core when Pando is down.
- [ ] Search hits are treated as candidate ids only: every field shown to a user is re-read from git-in-track's own index, never taken from Pando's document metadata.
- [ ] Search UI: semantic results merged with exact matches, labelled; item ids resolved back to items.
- [ ] Agent skill documents which tool answers which question (structured → gintrack MCP, semantic → `kb_search_documents`, code → `code_hybrid_search`).
- [ ] Settings card: Pando URL, corpus location, reindex now, index status.

## Notes

Pando has no REST search API today and no KB reindex trigger; `KBAutoImport` over a `KBPath` directory is the sync path, with the filesystem watcher deliberately disabled. Pin the embedding model: chunks with a different vector length are silently skipped, and the model is global to the Pando instance, so a change affects every consumer of it.

**Metadata round-trip contract (a known limitation, not a gate).** Pando's KB discards front-matter keys other than `tags`/`aliases`, and the watcher erases even those on the first edit (report A1/A2/A5/A7 — 523 of 539 documents in Pando's own KB have already lost their tags). The design above is built so this does not block the epic: the exporter writes front matter for human readability and for whatever survives, but nothing in git-in-track depends on reading `id`, `type`, `status`, `milestone`, `parent` or `updated` back out of a Pando search result. If PANDO-EP-0005 restores fidelity, that becomes an optimisation, not a prerequisite.

**Pando dependencies (plain ids):** PANDO-EP-0005 (KB metadata fidelity and REST search/reindex surface) — improves the corpus round-trip and turns "reindex now" into a real awaitable operation. PANDO-EP-0006 (authenticate the MCP HTTP transport) — `:9777` is unauthenticated with CORS `*` today. PANDO-EP-0007 (search scale and correctness) — KB vector search is a full scan per query, fine at ~10 000 chunks, not at ~35 000. Everything else in this epic is buildable today.

Transport: MCP client now, plain REST later behind the same `internal/pando.Client` seam, so the switch costs nothing above that package.
