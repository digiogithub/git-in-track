---
id: GIT-T-0203
type: task
title: Add the reindex endpoint with progress events
status: in_review
priority: medium
parent: GIT-US-0091
milestone: GIT-M-0013
author: mcp
labels: [server]
estimate: 3
created: 2026-09-13T13:20:27Z
updated: 2026-09-15T16:04:31Z
started: 2026-09-15T16:04:16Z
---

## Description

Add `POST /api/v1/search/reindex`, which re-runs the full corpus export and calls `code_index_project` for the repository source tree through `internal/pando.Client`, publishing progress on the hub in the shape `sync.progress` uses. A second call while one is running is refused rather than queued. Document in the handler that there is no KB reindex trigger in Pando: the corpus re-export plus Pando's own filesystem watcher is the mechanism.

## Acceptance Criteria

- [ ] A reindex re-exports the corpus, triggers the code index and publishes progress events.
- [ ] A concurrent call is refused with a clear problem code and the running job is unaffected.
- [ ] A Pando failure leaves the corpus export result intact and reports the code-index failure separately.
- [ ] `go test -race ./internal/server/...` covers the happy path, the concurrent refusal and the partial failure.
