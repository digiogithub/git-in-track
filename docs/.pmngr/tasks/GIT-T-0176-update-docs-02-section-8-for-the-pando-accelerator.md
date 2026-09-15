---
id: GIT-T-0176
type: task
title: Update docs/02 section 8 for the Pando accelerator
status: done
priority: medium
parent: GIT-US-0082
milestone: GIT-M-0013
author: mcp
labels: [docs, agent-ok]
estimate: 1
created: 2026-09-13T13:19:35Z
updated: 2026-09-15T16:43:51Z
started: 2026-09-15T16:04:13Z
closed: 2026-09-15T16:43:51Z
---

## Description

Extend the search design section of `docs/02-architecture.md`, which currently names bleve as the worked example of an optional native accelerator behind the `core/search` interface, to name Pando alongside it: what it adds (hybrid lexical plus embedding ranking over an exported corpus), why it stays optional (`internal/core` must compile to WASM, ADR-003), and how a client learns which backend answered. Document the capability value in `docs/07-cli-and-api.md` and add a `CHANGELOG.md` entry.

## Acceptance Criteria

- [ ] Section 8 names Pando as an optional native accelerator with the same reasoning bleve is given, and says browser mode always uses the core index.
- [ ] The `fullTextSearch` capability value is documented in `docs/07-cli-and-api.md`.
- [ ] `CHANGELOG.md` records the backend.
