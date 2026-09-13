---
id: GIT-T-0084
type: task
title: Add the issue search endpoint with cursor paging
status: done
priority: medium
parent: GIT-US-0054
milestone: GIT-M-0011
author: mcp
labels: [server, agent-ok]
estimate: 3
created: 2026-09-13T13:17:23Z
updated: 2026-09-13T16:03:41Z
started: 2026-09-13T16:03:25Z
closed: 2026-09-13T16:03:41Z
---

## Description

Add `internal/server/youtrack.go` with `GET /api/v1/youtrack/issues?q=&preset=&limit=&cursor=`, registered in `internal/server/api.go` next to the other v1 routes and gated on `features.youtrack` and a linked project (404 otherwise). The handler composes the query with `ComposeQuery`, pages with `$top`/`$skip` behind an opaque cursor, and returns `{items: [{id, idReadable, summary, type, state, assignee, updated, url}], nextCursor}`. Requests pass through the shared rate limiter. Upstream errors and timeouts map to a documented error body with no token or Authorization header in it.

## Acceptance Criteria

- [ ] The route exists, is gated, and returns 404 for an unlinked project or a disabled feature.
- [ ] Paging is stable and the cursor is opaque.
- [ ] Upstream 4xx, 5xx and timeouts map to a documented error body with no credential leakage.
- [ ] `go test -race ./internal/server/...` covers paging, gating and the error mapping with an httptest upstream.
