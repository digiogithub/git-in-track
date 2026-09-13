---
id: GIT-T-0145
type: task
title: Implement GET /api/v1/sync/jobs with filtering and paging
status: todo
priority: medium
parent: GIT-US-0078
milestone: GIT-M-0011
author: mcp
labels: [server, agent-ok]
estimate: 3
created: 2026-09-13T13:18:51Z
updated: 2026-09-13T13:18:51Z
---

## Description

Extend `mountSync` (`internal/server/sync.go:94`) with `GET /api/v1/sync/jobs`, filtering by state and kind, returning a bounded page with a cursor in the shape the other list endpoints use, and `GET /api/v1/sync/jobs/{id}` returning one job with its attempt count, next attempt time and redacted last error. Keep the handler thin and read straight from the engine.

## Acceptance Criteria

- [ ] Filtering by state and kind works and the page size is bounded.
- [ ] An unknown job id returns a `not_found` problem document.
- [ ] No response contains a credential.
- [ ] `go test -race ./internal/server/...` covers listing, filtering and the single-job read.
