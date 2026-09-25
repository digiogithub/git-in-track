---
id: GIT-US-0150
type: story
title: Persist the verification cache in IndexedDB in browser-only mode
status: in_review
priority: low
parent: GIT-EP-0027
milestone: GIT-M-0015
author: mcp
labels: [web, wasm, agent-ok]
estimate: 2
created: 2026-09-24T21:55:53Z
updated: 2026-09-24T22:53:32Z
---

## Description

GIT-US-0141 (PR #59) added `core.VerifyCache`, which is WASM-clean and has a file store and an in-memory store. ADR-037 §4 says the browser keeps it in IndexedDB, but nothing in `web/src` persists it yet.

## Acceptance Criteria

- [x] An IndexedDB-backed store behind the same core interface, rebuildable and never the source of truth. Reads and writes are wrapped so that a blocked or empty IndexedDB never breaks the page.
- [x] It is used by whatever browser-only path reads verification evidence. If browser-only mode still has no coverage host, document what it enables.
- [x] Vitest tests with a fake IndexedDB. docs/05 is updated.
