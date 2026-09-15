---
id: GIT-T-0192
type: task
title: Add the search.semantic method to the vault dispatch table
status: done
priority: medium
parent: GIT-US-0088
milestone: GIT-M-0013
author: mcp
labels: [core, server]
estimate: 3
created: 2026-09-13T13:20:00Z
updated: 2026-09-15T16:43:40Z
started: 2026-09-15T16:17:17Z
closed: 2026-09-15T16:43:40Z
---

## Description

Add a `search.semantic` case to the workspace dispatch table (`internal/vault/dispatch.go:73-260`) taking a query, an optional limit and an optional project, and returning hits with id, title, score, origin and snippet. The implementation delegates to the searcher the server injected, so the business logic lives here rather than in `internal/mcp`. When no semantic backend is wired, it returns a typed error naming the reason instead of quietly falling back to substring search.

## Acceptance Criteria

- [ ] `search.semantic` is dispatchable and returns the documented shape.
- [ ] Without a semantic backend it returns a typed error, never core results.
- [ ] `go test -race ./internal/vault/...` covers a hit, an empty result and the unavailable backend.
