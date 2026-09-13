---
id: GIT-T-0014
type: task
title: Document the YouTrack client package and its limits
status: todo
priority: medium
parent: GIT-US-0046
milestone: GIT-M-0011
author: mcp
labels: [docs]
estimate: 1
created: 2026-09-13T13:15:35Z
updated: 2026-09-13T13:15:35Z
---

## Description

Write the package doc comment for `internal/youtrack` covering the auth header, the context-path rule (never `url.ResolveReference`), the bare-array list contract, the `order by:` paging rule, the rate limit and retry policy, and the untrusted-content warning. Add a short section to `docs/02-architecture.md` placing the package among the native-only packages beside `internal/gitops`, and a `CHANGELOG.md` entry.

## Acceptance Criteria

- [ ] The package doc comment covers all six rules above and is written in English.
- [ ] `docs/02-architecture.md` lists `internal/youtrack` as native-only and explains why it cannot live in `internal/core`.
- [ ] `make lint` passes and `CHANGELOG.md` is updated.
