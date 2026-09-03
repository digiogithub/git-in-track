---
id: GIT-EP-0002
type: epic
title: Browser-only MVP
status: in_progress
created: 2026-09-03T00:00:00Z
updated: 2026-09-03T21:47:06Z
started: 2026-09-03T21:47:06Z
author: team
priority: critical
milestone: GIT-M-0002
estimate: 37
labels: [web, wasm, core]
links:
  - kind: blocked_by
    target: GIT-EP-0001
---

## Description

Phase 1. Deliver a useful product with no installation at all: the user opens a local
project folder in a Chromium browser, reads the knowledge base rendered from Markdown, and
manages a single project's backlog in `.pmngr/`.

Indexing and parsing run in the WebAssembly build of the shared Go core, inside a Web
Worker, with the index cached in IndexedDB. Writes go straight to the working tree through
File System Access handles. Browsers without that API get an honest read-only fallback.

## Acceptance Criteria

- [ ] A new user opens `testdata/fixtures/project-basic` and creates, edits and reads a
      story without touching a terminal, in under two minutes.
- [ ] A 5,000-file vault indexes in under 3 seconds and re-indexes one changed file in
      under 100 ms.
- [ ] The UI thread is never blocked for more than 50 ms during indexing.
- [ ] Every file written by the browser is read back identically by the native Go parser.
- [ ] Firefox and Safari load a vault read-only and explain the limitation clearly.

## Notes

Stories: GIT-US-0006 … GIT-US-0011. See docs/11-roadmap.md, milestone 2.
Primary risks: R1 (File System Access support), R2 (WASM performance).
