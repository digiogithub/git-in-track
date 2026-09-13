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
updated: 2026-09-13T14:09:22Z
---

## Description

Write the package doc comment for `internal/youtrack` covering the auth header, the context-path rule (never `url.ResolveReference`), the bare-array list contract, the `order by:` paging rule, the rate limit and retry policy, and the untrusted-content warning. Add a short section to `docs/02-architecture.md` placing the package among the native-only packages beside `internal/gitops`, and a `CHANGELOG.md` entry.

## Acceptance Criteria

- [x] The package doc comment covers all six rules above and is written in English.
- [ ] `docs/02-architecture.md` lists `internal/youtrack` as native-only and explains why it cannot live in `internal/core`.
- [ ] `make lint` passes and `CHANGELOG.md` is updated.

## Notes

Partially done — this task stays open for the docs half.

Done: `internal/youtrack/doc.go` covers the six rules as a numbered list — bearer auth with the `perm:` prefix and no token in any rendering; the context-path concatenation rule with the explicit ban on `url.ResolveReference`; the bare-array list contract; the `order by:` paging rule with short-page exhaustion; the shared 5 req/s limiter and the retry policy with `Retry-After` and drained bodies; and the untrusted-content warning covering issue descriptions, comment text and article content. Linting is clean for the package: `golangci-lint v2.13.2 run ./internal/youtrack/...` reports 0 issues.

Still owed: `docs/02-architecture.md` and `CHANGELOG.md`. Both were **outside the implementing agent's file ownership** in this wave — other agents were editing the docs tree at the same time, so the assignment restricted the work to `internal/youtrack/`. The package doc comment is the source text to copy from. `make lint` over the whole module was not run for the same reason: `internal/core` was mid-edit during the pass.
