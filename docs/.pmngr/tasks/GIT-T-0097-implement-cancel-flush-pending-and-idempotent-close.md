---
id: GIT-T-0097
type: task
title: Implement Cancel, Flush, Pending and idempotent Close
status: done
priority: medium
parent: GIT-US-0063
milestone: GIT-M-0011
author: mcp
labels: [server, agent-ok]
estimate: 3
created: 2026-09-13T13:17:41Z
updated: 2026-09-13T14:06:19Z
started: 2026-09-13T14:06:06Z
closed: 2026-09-13T14:06:19Z
---

## Description

Implement the lifecycle methods: `Cancel(id)` removing a queued job or cancelling a running one through its per-job context, `Pending()` reporting counts by state, `Flush(ctx)` waiting for the queue to drain within the caller's deadline, and `Close(ctx)` stopping the workers, safe and idempotent when called twice. Follow `Committer.Flush` (`internal/gitops/committer.go:240`), `Pending` (`:267`) and `Close` (`:277`).

## Acceptance Criteria

- [x] `Cancel` works for both queued and running jobs and is a no-op for an unknown id.
- [x] `Flush` returns when the queue is empty or the context expires, and never blocks forever.
- [x] `Close` is idempotent, stops every goroutine and makes subsequent `Enqueue` calls fail clearly.
- [x] `go test -race ./internal/syncengine/...` covers double close and cancel-during-run.

## Notes

`Cancel` returns a bool rather than an error: an unknown id, and a job that already finished, are both "nothing to cancel" and not failures. `Flush` on an engine that was never started returns `ErrNotStarted` instead of blocking until the deadline.
