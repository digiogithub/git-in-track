---
id: GIT-M-0002
type: milestone
title: Phase 1 — Browser-only MVP
status: done
created: 2026-09-03T00:00:00Z
updated: 2026-09-04T05:06:40Z
closed: 2026-09-04T05:06:40Z
started: 2026-09-03T21:47:06Z
author: team
labels: [web, wasm]
links:
  - kind: relates_to
    target: GIT-EP-0002
custom:
  phase: 1
  version: v0.2.0
---

## Description

Phase 1 of the roadmap. A useful product with zero installation: open a local folder in a
Chromium browser, read the knowledge base, and manage one project's backlog. Indexing runs
in the WASM core inside a Web Worker; the index is cached in IndexedDB.

## Acceptance Criteria

- [ ] Open, create, edit and read a story in under two minutes, no terminal.
- [ ] 5,000-file vault indexes in under 3 s; single-file re-index under 100 ms.
- [ ] UI thread never blocked for more than 50 ms while indexing.
- [ ] Files written in the browser parse identically natively.
- [ ] Firefox and Safari load read-only and say why.

## Notes

Epic: GIT-EP-0002. Stories: GIT-US-0006 … GIT-US-0011. Risks: R1, R2.
