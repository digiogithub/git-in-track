---
id: GIT-T-0048
type: task
title: Implement the idempotent create-or-update write path
status: done
priority: medium
parent: GIT-US-0047
milestone: GIT-M-0011
author: mcp
labels: [core, server, agent-ok]
estimate: 3
created: 2026-09-13T13:16:28Z
updated: 2026-09-13T15:08:13Z
started: 2026-09-13T15:07:58Z
closed: 2026-09-13T15:08:13Z
---

## Description

In `internal/vault/youtrack.go`, implement `youtrack.import.run`: for each mapped issue, look the item up by `(external.system, external.id)` through the index, then call `core.FileStore.Update` (`internal/core/store.go:423`) with an `ItemPatch` when it exists or `Create` (`:321`) when it does not, keeping the existing gintrack id on an update. Collect everything into one `WriteSet` with `v.fs.begin()` and `v.commit(ctx)` (`vault.go:1511`) so the batch lands as commits, index updates and WebSocket events. A failure on one issue records `{youtrackId, error}` and continues.

## Acceptance Criteria

- [x] An unknown issue creates an item; a known one updates it in place and keeps its id.
- [x] The whole batch commits through one `WriteSet` and produces `item.changed` and `index.updated` events.
- [x] A single failing issue does not abort the batch and is reported in the result list.
- [x] `go test -race ./internal/vault/...` covers create, re-import update and the partial-failure path.

## Notes

`resolveTargets` looks every issue of the batch up by `(youtrack, idReadable)` once; an issue that resolves is an update that keeps the item's id, and everything else is a create whose id is allocated **before** any write, so relations inside the batch resolve. The status of an update is applied through `MoveWith(..., Force: true)` rather than through the patch: the remote tracker is the authority on the state of an issue it owns, so a transition the local workflow does not declare is forced rather than refused.

The batch runs between one `v.fs.begin()` and one `v.commit(ctx)`, so it lands as a single `WriteSet` and one index update. A failing issue is recorded as `{youtrackId, itemId, action, error}` and the loop continues; an issue the tracker would not hand over is reported the same way instead of aborting resolution. Covered by `TestYouTrackImportIsIdempotent` and `TestYouTrackImportReportsOneFailureAndContinues`.
