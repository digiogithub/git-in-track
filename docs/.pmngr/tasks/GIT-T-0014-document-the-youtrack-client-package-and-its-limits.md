---
id: GIT-T-0014
type: task
title: Document the YouTrack client package and its limits
status: done
priority: medium
parent: GIT-US-0046
milestone: GIT-M-0011
author: mcp
labels: [docs]
estimate: 1
created: 2026-09-13T13:15:35Z
updated: 2026-09-13T14:31:02Z
started: 2026-09-13T14:30:52Z
closed: 2026-09-13T14:31:02Z
---

## Description

Write the package doc comment for `internal/youtrack` covering the auth header, the context-path rule (never `url.ResolveReference`), the bare-array list contract, the `order by:` paging rule, the rate limit and retry policy, and the untrusted-content warning. Add a short section to `docs/02-architecture.md` placing the package among the native-only packages beside `internal/gitops`, and a `CHANGELOG.md` entry.

## Acceptance Criteria

- [x] The package doc comment covers all six rules above and is written in English.
- [x] `docs/02-architecture.md` lists `internal/youtrack` as native-only and explains why it cannot live in `internal/core`.
- [ ] `make lint` passes and `CHANGELOG.md` is updated.

## Notes

Done: `internal/youtrack/doc.go` covers the six rules as a numbered list — bearer auth with the `perm:` prefix and no token in any rendering; the context-path concatenation rule with the explicit ban on `url.ResolveReference`; the bare-array list contract; the `order by:` paging rule with short-page exhaustion; the shared 5 req/s limiter and the retry policy with `Retry-After` and drained bodies; and the untrusted-content warning covering issue descriptions, comment text and article content.

Docs half closed in a later pass: `docs/02-architecture.md` §6 gains an `internal/youtrack/` line in the layout block and a table row beside `internal/gitops`, carrying the six rules and stating plainly that nothing imports the package yet; `CHANGELOG.md` gains an Unreleased → Added entry saying the same. The third criterion stays unticked because `make lint` does not pass in the shared working tree: `golangci-lint` reports 9 issues in files other agents are writing right now (`internal/vault/inbox.go`, `internal/youtrack/mapping/`). No Go file was touched by the docs pass, and `make lint-web` and `make lint-ci` are clean.
