---
id: GIT-T-0042
type: task
title: Serve the inbox list and triage endpoints with If-Match and WS events
status: done
priority: medium
parent: GIT-US-0056
milestone: GIT-M-0012
author: mcp
labels: [server]
estimate: 3
created: 2026-09-13T13:16:20Z
updated: 2026-09-13T15:17:43Z
started: 2026-09-13T15:17:18Z
closed: 2026-09-13T15:17:43Z
---

## Description

Add `GET /api/v1/inbox` and `POST /api/v1/items/{id}/triage` to `internal/server/items.go:19-36`, both thin wrappers over `s.call(...)` (`internal/server/api.go:134-156`). The listing paginates like the other listings (`maxItemsPerPage = 500`) and accepts `project`, `status` and `cursor`; the triage route requires `If-Match` via `requireIfMatch` (`internal/server/api.go:189`) and maps a stale rev to 412. Publish an `inbox.changed` event from `internal/server/events.go` carrying `{project, id, action, pendingCount}` alongside the existing `publishWrite`.

## Acceptance Criteria

- [x] Both routes are mounted, answer the documented shapes and reject a body larger than `maxRequestBody`.
- [x] A triage call with a missing `If-Match` gets `precondition_required` and one with a stale rev gets 412 carrying the current rev.
- [x] `inbox.changed` is published on every triage and every inbox create, and is subscribable on `GET /api/v1/events`.
- [x] `go test -race ./internal/server/...` covers the listing, pagination, the 412 and the event.

## Notes

Implemented in `internal/server/inbox.go` (new); `GET /api/v1/inbox` is mounted
in `mountAPI` and `POST /items/{id}/triage` in `mountItems`. Both go through
`s.call`, so the body limit and the problem mapping are the shared ones.

`If-Match: *` reaches the vault as the **empty string**, never as the literal
star — `requireIfMatch` already returns `""` for the wildcard, and the test
`TestInboxTriageAcceptsTheWildcardPrecondition` pins it.

`no_triage_status` is registered in `problem.go` as a **409**: a project that
declares no triage status has no inbox, which the user fixes in `project.yaml`
rather than by retrying. Listing such a project is not an error — the queue is
simply empty; filing into it is.

A triage publishes `item.changed`, `index.updated`, the commit-on-save write set
(a `duplicate` decision writes two files, staged together) and `inbox.changed`
with the pending count the vault returned. A create that carries an `inbox`
block publishes `inbox.changed` too, reading the count with one extra
`inbox.list` whose failure is not fatal. Documented in docs/07 §5.5 and §5.6.
