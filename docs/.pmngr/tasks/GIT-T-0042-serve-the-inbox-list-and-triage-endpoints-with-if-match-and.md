---
id: GIT-T-0042
type: task
title: Serve the inbox list and triage endpoints with If-Match and WS events
status: todo
priority: medium
parent: GIT-US-0056
milestone: GIT-M-0012
author: mcp
labels: [server]
estimate: 3
created: 2026-09-13T13:16:20Z
updated: 2026-09-13T13:16:20Z
---

## Description

Add `GET /api/v1/inbox` and `POST /api/v1/items/{id}/triage` to `internal/server/items.go:19-36`, both thin wrappers over `s.call(...)` (`internal/server/api.go:134-156`). The listing paginates like the other listings (`maxItemsPerPage = 500`) and accepts `project`, `status` and `cursor`; the triage route requires `If-Match` via `requireIfMatch` (`internal/server/api.go:189`) and maps a stale rev to 412. Publish an `inbox.changed` event from `internal/server/events.go` carrying `{project, id, action, pendingCount}` alongside the existing `publishWrite`.

## Acceptance Criteria

- [ ] Both routes are mounted, answer the documented shapes and reject a body larger than `maxRequestBody`.
- [ ] A triage call with a missing `If-Match` gets `precondition_required` and one with a stale rev gets 412 carrying the current rev.
- [ ] `inbox.changed` is published on every triage and every inbox create, and is subscribable on `GET /api/v1/events`.
- [ ] `go test -race ./internal/server/...` covers the listing, pagination, the 412 and the event.
