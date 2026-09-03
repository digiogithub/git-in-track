---
id: GIT-US-0007
type: story
title: Index a vault in a Web Worker and cache it in IndexedDB
status: backlog
created: 2026-09-03T00:00:00Z
updated: 2026-09-03T00:00:00Z
author: team
priority: critical
parent: GIT-EP-0002
milestone: GIT-M-0002
estimate: 8
labels: [web, wasm, core, performance]
links:
  - kind: blocked_by
    target: GIT-US-0006
  - kind: blocked_by
    target: GIT-US-0002
---

## Description

As a user with a large repository, I want the app to stay responsive while it reads my
files, so that opening a real project does not freeze the browser tab.

The WASM build of the shared core runs inside a dedicated Web Worker behind a typed
request/response channel. The worker walks the vault through the File System Access
handles, parses items and knowledge-base pages, builds the index and the link graph, and
streams progress back to the UI. The result is cached in IndexedDB keyed by a content hash
so a second visit is near-instant, and invalidated per file rather than wholesale.

This story owns the Phase 1 performance budget and directly addresses risk R2.

## Acceptance Criteria

- [ ] The WASM core loads and runs in a Web Worker; the main thread never parses.
- [ ] A 5,000-file vault indexes in under 3 seconds on a mid-range laptop.
- [ ] Re-indexing a single changed file takes under 100 ms.
- [ ] No main-thread task exceeds 50 ms during indexing, measured with the Long Tasks API.
- [ ] Indexing progress is reported and the operation is cancellable.
- [ ] The index is cached in IndexedDB and restored on reopen without a full re-scan.
- [ ] Cache entries are invalidated per file by content hash.
- [ ] `core.wasm` is under 6 MB compressed and is fetched after first paint.
- [ ] A CI benchmark job fails on a regression against the budget.
