---
id: GIT-T-0048
type: task
title: Implement the idempotent create-or-update write path
status: todo
priority: medium
parent: GIT-US-0047
milestone: GIT-M-0011
author: mcp
labels: [core, server, agent-ok]
estimate: 3
created: 2026-09-13T13:16:28Z
updated: 2026-09-13T13:16:28Z
---

## Description

In `internal/vault/youtrack.go`, implement `youtrack.import.run`: for each mapped issue, look the item up by `(external.system, external.id)` through the index, then call `core.FileStore.Update` (`internal/core/store.go:423`) with an `ItemPatch` when it exists or `Create` (`:321`) when it does not, keeping the existing gintrack id on an update. Collect everything into one `WriteSet` with `v.fs.begin()` and `v.commit(ctx)` (`vault.go:1511`) so the batch lands as commits, index updates and WebSocket events. A failure on one issue records `{youtrackId, error}` and continues.

## Acceptance Criteria

- [ ] An unknown issue creates an item; a known one updates it in place and keeps its id.
- [ ] The whole batch commits through one `WriteSet` and produces `item.changed` and `index.updated` events.
- [ ] A single failing issue does not abort the batch and is reported in the result list.
- [ ] `go test -race ./internal/vault/...` covers create, re-import update and the partial-failure path.
