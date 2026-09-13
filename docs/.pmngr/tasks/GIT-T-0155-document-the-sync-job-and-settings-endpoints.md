---
id: GIT-T-0155
type: task
title: Document the sync job and settings endpoints
status: todo
priority: medium
parent: GIT-US-0078
milestone: GIT-M-0011
author: mcp
labels: [docs]
estimate: 1
created: 2026-09-13T13:19:03Z
updated: 2026-09-13T13:19:03Z
---

## Description

Document the six endpoints in `docs/07-cli-and-api.md` beside the existing sync section, with request and response examples, the problem codes, the valid state transitions for retry and cancel, and the `persisted` semantics. Add a `CHANGELOG.md` entry.

## Acceptance Criteria

- [ ] Every endpoint has parameters, an example response and an error table.
- [ ] The state-transition rules for retry and cancel are documented.
- [ ] `make lint` passes and `CHANGELOG.md` is updated.
