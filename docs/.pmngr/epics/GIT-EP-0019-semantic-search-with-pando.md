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
updated: 2026-09-13T13:08:37Z
---

## Description

git-in-track's search is a weighted substring matcher (`core.Index.Search`). Pando has hybrid BM25 + embedding search over a KB corpus and a code index, reachable only through MCP. This epic wires the two: the companion exports every item and KB page as a Markdown document with front matter into a corpus directory (`.pando-kb/` in the cache dir, one file per item, id and labels as tags, wikilinks preserved) and keeps it fresh from the watcher's change events; Pando indexes it with `[Remembrances] KBPath` + `KBWatch`. The companion then queries `kb_search_documents` (and `code_hybrid_search` for the source tree) over Pando's MCP HTTP transport and offers the results as an optional search backend, reported through the existing `fullTextSearch` capability, in the search UI and to the agent as a tool.

## Acceptance Criteria

- [ ] Exporter: full export on start, incremental on `item.changed` / `file.changed` events, deletions pruned; never points Pando at the repository root.
- [ ] Companion MCP client to `pando mcp-server` (streamable HTTP, loopback), health and timeouts.
- [ ] `core/search` contract gains a native `pando` implementation; capability `fullTextSearch: 'core' | 'bleve' | 'pando'`; fallback to core when Pando is down.
- [ ] Search UI: semantic results merged with exact matches, labelled; item ids resolved back to items.
- [ ] Agent skill documents which tool answers which question (structured → gintrack MCP, semantic → `kb_search_documents`, code → `code_hybrid_search`).
- [ ] Settings card: Pando URL, corpus location, reindex now, index status.

## Notes

Pando has no REST search API and no KB reindex trigger; the filesystem watcher is the sync path. Pin the embedding model: chunks with a different vector length are silently skipped.
