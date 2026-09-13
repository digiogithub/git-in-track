---
id: GIT-US-0082
type: story
title: Pando search backend behind the core search contract
status: backlog
priority: high
parent: GIT-EP-0019
milestone: GIT-M-0013
author: mcp
labels: [core, server, web, docs]
estimate: 8
created: 2026-09-13T13:14:19Z
updated: 2026-09-13T13:14:19Z
---

## Description

As a user searching a large backlog, I want results ranked by meaning as well as by substring, so that "how do we handle a stale write" finds the rev protocol stories even when none of them uses those words.

`docs/02-architecture.md` §8 already settles the architecture for this exact case, with bleve as the worked example: an external engine is only ever *an optional native accelerator behind the same `core/search` interface, never the only backend*, because `internal/core` must stay WASM-clean (ADR-003). Follow it literally. Keep `core.Index.Search` (`internal/core/query.go:544`, returning `core.SearchHit` :518) as the contract, add a `Searcher` interface in `internal/core` that the index already satisfies, and add a native implementation in `internal/server` that wraps `internal/pando.Client` and maps `file_path` back to item ids and KB page paths.

Selection happens in `internal/server`: when a Pando URL is configured, reachable and the corpus has been exported, `GET /api/v1/search` (`internal/server/api.go:75`) serves the Pando backend, otherwise the core index — a per-request decision, so a Pando outage degrades instead of failing. Merge deterministically: exact id and title matches from the core index come first in their existing order, then semantic hits not already present, each flagged with its origin and score so the UI can label them. Extend the existing capability rather than adding a parallel flag: `features.search` gains `"pando"` (`internal/server/server.go:411-435`) and `Capabilities.fullTextSearch` becomes `'core' | 'bleve' | 'pando'` in `web/src/api/provider.ts:689` with the mapping in `companion-provider.ts:761`.

## Acceptance Criteria

- [ ] A `Searcher` contract exists in `internal/core` and `core.Index` satisfies it with no behaviour change; `make wasm` still builds.
- [ ] A Pando-backed searcher lives in native-only code and resolves `file_path` results back to item ids and KB page paths, dropping anything unresolvable.
- [ ] `GET /api/v1/search` returns merged results: exact core matches first in their current order, then semantic hits, each carrying its origin and score.
- [ ] With Pando unreachable or the corpus missing, the endpoint answers from the core index with the same response shape and logs the degradation once, not per request.
- [ ] `features.search` reports `"pando"` only when the backend is actually selected, and `fullTextSearch` accepts `'pando'` across all four providers.
- [ ] Search latency with Pando selected stays within the `docs/02-architecture.md` §9 budget, or the backend falls back.
- [ ] `go test -race ./internal/core/... ./internal/server/...` covers selection, merge order, id resolution and the fallback path.
- [ ] `docs/02-architecture.md` §8 is updated to name Pando alongside bleve as an optional native accelerator, and `docs/07-cli-and-api.md` documents the capability value.

## Notes

The existing search is a weighted substring matcher where every term must match, scoring id 100 / title 3 / label 2 / body 1 (`internal/core/query.go:544`). That is exactly the gap Pando fills — keep both rather than replacing one with the other.

Two hard constraints from the reports: `internal/core` must compile to WASM, so no `net/http` there (AGENTS.md; `internal/core/doc.go:11-12`); and browser-only mode has no reach to a local Pando at all, so `browser-provider.ts` keeps reporting `'core'`.

Ranking caveat: Pando KB scores are RRF fusion values around 0.016 while boosted code scores approach 1.0 — never sort the two together numerically. Its KB vector search is a full scan of every embedded chunk per query in Go with no ANN (`kb.go:833-896`); fine for thousands of chunks, so measure before promising anything at ten thousand.

Do not add a second capability flag next to `fullTextSearch`, and do not make Pando a hard dependency of the search endpoint.
