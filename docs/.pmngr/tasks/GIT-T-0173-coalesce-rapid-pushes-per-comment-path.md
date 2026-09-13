---
id: GIT-T-0173
type: task
title: Coalesce rapid pushes per comment path
status: done
priority: medium
parent: GIT-US-0072
milestone: GIT-M-0011
author: mcp
labels: [server, agent-ok]
estimate: 2
created: 2026-09-13T13:19:29Z
updated: 2026-09-13T16:28:55Z
started: 2026-09-13T16:28:32Z
closed: 2026-09-13T16:28:55Z
---

## Description

Debounce and coalesce enqueues on the comment path so a burst of edits produces one push, following the coalescing shape of `internal/gitops/committer.go:157-211` (coalesce key, `arm`, `time.AfterFunc`, `fire`) rather than inventing a new mechanism. Use a fake clock in tests so the debounce is deterministic.

## Acceptance Criteria

- [x] Repeated writes to the same comment path within the debounce window produce one push job.
- [x] Writes to different comment paths are not coalesced together.
- [x] `go test -race ./internal/server/...` verifies coalescing with a fake clock.

## Notes

No second timer was added: the `arm`/`AfterFunc`/`fire` window this task describes already exists one layer down, in `syncengine.Engine.addToBatchLocked`, keyed on exactly the (kind, key) pair the vault emits. A copy in `internal/server` would be a second window to drift out of step with it.

What the server added is the missing half. The engine folds queued jobs into one *batch*, and a batch of three jobs for one comment is still three deliveries, so the push would create the comment and then edit it twice for nothing. `coalescedJobID` (`internal/server/youtrackjobs.go`) gives the three path-keyed kinds — comment push, KB publish, KB pull — a stable idempotence key, so a burst is one job. The import kind is deliberately excluded: its key is a query, and two imports of the same query a minute apart are two things a person asked for.

`SyncEngine` gained a `Clock` field so the debounce is driven rather than slept through; `TestCommentPushIsCoalescedPerCommentPath` and `TestCoalescedJobIDIsLimitedToPathKeyedKinds` cover it.
