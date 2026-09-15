---
id: GIT-US-0082
type: story
title: Pando search backend behind the core search contract
status: done
priority: high
parent: GIT-EP-0019
milestone: GIT-M-0013
author: mcp
labels: [core, server, web, docs]
estimate: 8
created: 2026-09-13T13:14:19Z
updated: 2026-09-15T17:00:47Z
started: 2026-09-15T16:04:21Z
closed: 2026-09-15T17:00:47Z
---

## Description

As a user searching a large backlog, I want results ranked by meaning as well as by substring, so that "how do we handle a stale write" finds the rev protocol stories even when none of them uses those words.

`docs/02-architecture.md` §8 already settles the architecture for this exact case, with bleve as the worked example: an external engine is only ever *an optional native accelerator behind the same `core/search` interface, never the only backend*, because `internal/core` must stay WASM-clean (ADR-003). Follow it literally. Keep `core.Index.Search` (`internal/core/query.go:628`, returning `core.SearchHit` `:602`) as the contract, add a `Searcher` interface in `internal/core` that the index already satisfies, and add a native implementation in `internal/server` that wraps `internal/pando.Client`.

**Pando returns candidates, never answers.** A hit gives a `file_path` and a score; the searcher maps that path back to an item id or KB page path and then **re-reads every field it will show — title, status, type, milestone, labels — from git-in-track's own index**. Pando's copy of the metadata is not authoritative and is not read (see GIT-US-0073: front matter beyond `tags`/`aliases` does not survive). An unresolvable hit is dropped.

**Multi-project queries: filter companion-side.** Pando has no project or path-prefix filter in its search options, so a corpus holding several repositories would under-return for a filtered query. Workaround, and it is the chosen design: **over-fetch** (`limit = 20`, the hard cap) and **filter by path prefix in the companion**, accepting that a query heavily dominated by another project's documents may under-return. No structured filtering is asked of Pando.

Selection happens in `internal/server`: when a Pando URL is configured, reachable and the corpus has been exported, `GET /api/v1/search` (`internal/server/api.go:79`) serves the Pando backend, otherwise the core index — a per-request decision, so a Pando outage degrades instead of failing. Merge deterministically: exact id and title matches from the core index come first in their existing order, then semantic hits not already present, each flagged with its origin and score so the UI can label them. Extend the existing capability rather than adding a parallel flag: `features.search` gains `"pando"` (`internal/server/server.go:444`, the current `"search": "core"` value at `:459`) and `Capabilities.fullTextSearch` becomes `'core' | 'bleve' | 'pando'` in `web/src/api/provider.ts:1179` (the `search(query)` method is at `:1493`) with the mapping in `companion-provider.ts:1022`.

## Acceptance Criteria

- [ ] A `Searcher` contract exists in `internal/core` and `core.Index` satisfies it with no behaviour change; `make wasm` still builds.
- [ ] A Pando-backed searcher lives in native-only code and resolves `file_path` results back to item ids and KB page paths, dropping anything unresolvable.
- [ ] Every field returned to the caller is re-read from git-in-track's own index; no field shown to a user comes from Pando's document metadata.
- [ ] Multi-project corpora are handled by over-fetching at `limit = 20` and filtering by path prefix companion-side; the under-return case is documented rather than silently wrong.
- [ ] `GET /api/v1/search` returns merged results: exact core matches first in their current order, then semantic hits, each carrying its origin and score.
- [ ] With Pando unreachable or the corpus missing, the endpoint answers from the core index with the same response shape and logs the degradation once, not per request.
- [ ] `features.search` reports `"pando"` only when the backend is actually selected, and `fullTextSearch` accepts `'pando'` across all four providers.
- [ ] Search latency with Pando selected stays within the measured budget below, or the backend falls back: the searcher refuses Pando above a configured chunk count rather than blocking the request.
- [ ] `go test -race ./internal/core/... ./internal/server/...` covers selection, merge order, id resolution, the prefix filter and the fallback path.
- [ ] `docs/02-architecture.md` §8 is updated to name Pando alongside bleve as an optional native accelerator, and `docs/07-cli-and-api.md` documents the capability value.

## Notes

The existing search is a weighted substring matcher where every term must match, scoring id 100 / title 3 / label 2 / body 1 (`internal/core/query.go:628`). That is exactly the gap Pando fills — keep both rather than replacing one with the other.

Two hard constraints: `internal/core` must compile to WASM, so no `net/http` there (AGENTS.md; `internal/core/doc.go:11-12`); and browser-only mode has no reach to a local Pando at all, so `browser-provider.ts` keeps reporting `'core'`.

**Ranking caveat.** Pando KB scores are RRF fusion values around 0.016 while boosted code scores approach 1.0 (`internal/rag/hybrid.go:118-121` sorts them with no normalisation) — never sort the two together numerically, and call `kb_search_documents` and `code_hybrid_search` separately rather than `hybrid_search_remembrances`.

**Performance, measured.** Pando's KB vector search is a full scan of every embedded chunk per query, in Go, with no ANN (`kb.go:833-896`). Measured: ~100-200 ms at around 10 000 chunks, and it does not hold at around 35 000. Set a hard budget and fall back to the core index above a configured chunk count. PANDO-EP-0007 is the fix.

`core.SearchHit` already carries `Snippet` (`internal/core/query.go:602-610`), so nothing needs widening beyond an origin discriminator.

The `file_path` a hit carries is the path in the exported corpus, which is why GIT-US-0073's path layout is a contract, not an implementation detail.

**Pando dependencies (plain ids):** PANDO-EP-0007 (search scale and correctness) — removes the chunk-count ceiling and the fallback. PANDO-EP-0005 (KB metadata fidelity and REST search surface) — would allow a server-side path-prefix filter instead of over-fetching, and a plain HTTP call instead of MCP. Neither blocks: the story is buildable today with the workaround above.

Do not add a second capability flag next to `fullTextSearch`, and do not make Pando a hard dependency of the search endpoint.
