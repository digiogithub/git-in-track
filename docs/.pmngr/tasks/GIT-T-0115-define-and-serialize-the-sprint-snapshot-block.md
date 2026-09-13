---
id: GIT-T-0115
type: task
title: Define and serialize the sprint snapshot block
status: todo
priority: medium
parent: GIT-US-0080
milestone: GIT-M-0012
author: mcp
labels: [core]
estimate: 3
created: 2026-09-13T13:18:09Z
updated: 2026-09-13T13:18:09Z
---

## Description

Add `core.SprintSnapshot` with `Version int`, `ClosedAt Timestamp`, `Totals`, `ByStatus`, `ByAssignee`, `ByLabel`, `Burndown []SnapshotPoint` and the `MetricsProvenance` it froze, and hang it off `Sprint` as `Snapshot *SprintSnapshot`. Register `snapshot` in `sprintKnownKeys` (`internal/core/sprint.go:100-105`), parse it in `ParseSprint` (`:202`) and emit it from `SerializeSprint` (`:240`) at a fixed place in the key order, keeping unknown keys inside the block through the existing `Extra` mechanism.

## Acceptance Criteria

- [ ] A sprint file with a `snapshot` block round-trips byte-identically, including an unknown key inside it.
- [ ] `version` defaults to 1 and an unknown future version parses without loss.
- [ ] The key-order test is updated and still pins the full order.
- [ ] `go test -race ./internal/core/...` passes and `make wasm` builds.
