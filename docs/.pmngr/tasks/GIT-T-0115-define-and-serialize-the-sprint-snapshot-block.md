---
id: GIT-T-0115
type: task
title: Define and serialize the sprint snapshot block
status: done
priority: medium
parent: GIT-US-0080
milestone: GIT-M-0012
author: mcp
labels: [core]
estimate: 3
created: 2026-09-13T13:18:09Z
updated: 2026-09-13T14:01:09Z
started: 2026-09-13T14:00:54Z
closed: 2026-09-13T14:01:09Z
---

## Description

Add `core.SprintSnapshot` with `Version int`, `ClosedAt Timestamp`, `Totals`, `ByStatus`, `ByAssignee`, `ByLabel`, `Burndown []SnapshotPoint` and the `MetricsProvenance` it froze, and hang it off `Sprint` as `Snapshot *SprintSnapshot`. Register `snapshot` in `sprintKnownKeys` (`internal/core/sprint.go:100-105`), parse it in `ParseSprint` (`:202`) and emit it from `SerializeSprint` (`:240`) at a fixed place in the key order, keeping unknown keys inside the block through the existing `Extra` mechanism.

## Acceptance Criteria

- [x] A sprint file with a `snapshot` block round-trips byte-identically, including an unknown key inside it.
- [x] `version` defaults to 1 and an unknown future version parses without loss.
- [x] The key-order test is updated and still pins the full order.
- [x] `go test -race ./internal/core/...` passes and `make wasm` builds.

## Notes

The block is emitted between `retro` and `created`, with a fixed inner key order (`version`, `closed_at`, `totals`, `by_status`, `by_assignee`, `by_label`, `burndown`, `provenance`) and the unmodelled keys sorted after it — the same rule the top-level front matter follows. An open sprint emits no block at all, so ADR-017 still governs everything before the close.
