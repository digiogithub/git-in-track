---
id: GIT-T-0145
type: task
title: Implement GET /api/v1/sync/jobs with filtering and paging
status: done
priority: medium
parent: GIT-US-0078
milestone: GIT-M-0011
author: mcp
labels: [server, agent-ok]
estimate: 3
created: 2026-09-13T13:18:51Z
updated: 2026-09-13T15:13:51Z
started: 2026-09-13T15:13:06Z
closed: 2026-09-13T15:13:51Z
---

## Description

Extend `mountSync` (`internal/server/sync.go:94`) with `GET /api/v1/sync/jobs`, filtering by state and kind, returning a bounded page with a cursor in the shape the other list endpoints use, and `GET /api/v1/sync/jobs/{id}` returning one job with its attempt count, next attempt time and redacted last error. Keep the handler thin and read straight from the engine.

## Acceptance Criteria

- [x] Filtering by state and kind works and the page size is bounded.
- [x] An unknown job id returns a `not_found` problem document.
- [x] No response contains a credential.
- [x] `go test -race ./internal/server/...` covers listing, filtering and the single-job read.

## Notes

Implemented in `internal/server/syncjobs.go`, mounted from `mountSync` through
`mountSyncJobs`. `state` and `kind` are repeatable and OR within a field; an
unknown state is refused with `invalid_request` rather than matching nothing.
The page defaults to 100 and is capped at `maxItemsPerPage` (500); the cursor is
the id the next page starts at, which is stable because the engine hands its
jobs back in creation order. The unknown-id code is the new, more specific
`sync_job_not_found`, which maps to 404 like `not_found`. The rendered job is
`syncengine.Job` **without its payload**, which is the one field this layer
cannot vouch for; `lastError.message` is what the engine already redacted.
