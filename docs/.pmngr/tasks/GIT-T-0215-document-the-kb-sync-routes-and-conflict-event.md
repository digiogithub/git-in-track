---
id: GIT-T-0215
type: task
title: Document the KB sync routes and conflict event
status: todo
priority: medium
parent: GIT-US-0090
milestone: GIT-M-0011
author: mcp
labels: [docs, agent-ok]
estimate: 2
created: 2026-09-13T13:21:22Z
updated: 2026-09-13T13:21:22Z
---

## Description

Document the three routes and the per-page status shape in `docs/07-cli-and-api.md` §4, and the `youtrack.kb.conflict` event next to the `sync.job.*` events in §5.6. Explain the conflict semantics — last writer wins per direction, both-changed produces `<page>.conflict.md` — where a reader of the KB docs will find them. Add a `CHANGELOG.md` entry.

## Acceptance Criteria

- [ ] The routes and the status shape are documented in `docs/07-cli-and-api.md` §4.
- [ ] The conflict event and semantics are documented in §5.6 and in the KB documentation.
- [ ] `CHANGELOG.md` has an entry and `make lint` passes.
