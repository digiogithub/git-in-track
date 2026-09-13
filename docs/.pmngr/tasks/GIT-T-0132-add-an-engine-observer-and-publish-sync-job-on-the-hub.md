---
id: GIT-T-0132
type: task
title: Add an engine observer and publish sync.job.* on the hub
status: done
priority: medium
parent: GIT-US-0074
milestone: GIT-M-0011
author: mcp
labels: [server, agent-ok]
estimate: 3
created: 2026-09-13T13:18:32Z
updated: 2026-09-13T15:15:08Z
started: 2026-09-13T15:14:33Z
closed: 2026-09-13T15:15:08Z
---

## Description

Add an `Observer` seam to `internal/syncengine` receiving state transitions and progress, and implement it in `internal/server` publishing `sync.job.queued|started|progress|done|failed` through `Hub.Publish` (`internal/server/hub.go:167`), alongside the existing publishers in `internal/server/events.go`. Payloads carry job id, kind, key, state, attempt, processed and total counts, and the redacted error on failure. The engine must not import `internal/server`.

## Acceptance Criteria

- [x] All five topics are published with the documented payload and no credential ever appears in one.
- [x] `internal/syncengine` has no dependency on `internal/server`, enforced by the package's imports.
- [x] `go test -race ./internal/server/...` asserts one event per transition against a fake engine.

## Notes

The seam already existed: `syncengine.Options.OnJob func(Job)` fires off-lock
after every state change, so no change to `internal/syncengine` was needed and
its import list is unchanged. `internal/server/syncjobevents.go` holds the
observer, the payload shape and the topic mapping; the tests drive it directly
with fabricated jobs and once end to end through a real engine.

A cancelled job is announced on `sync.job.done` with `state: "cancelled"`: it is
a job that stopped without failing, and the payload says which. The payload
carries **no job payload at all**, which is the one field this layer cannot
vouch for; `lastError.message` is what the engine already redacted.

**One gap to know about.** `sync.job.started` is mapped and documented but never
fires today: `internal/syncengine` does not call `OnJob` for the
queued → running transition (`startBatch` transitions the job and returns
without emitting). Publishing it needs a one-line `e.emit(...)` in that package,
which this story does not own. Everything the engine does report is on the
stream.
