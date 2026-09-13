---
id: GIT-T-0096
type: task
title: Add SprintStatus and the pure DerivedStatus function
status: done
priority: medium
parent: GIT-US-0075
milestone: GIT-M-0012
author: mcp
labels: [core, agent-ok]
estimate: 3
created: 2026-09-13T13:17:39Z
updated: 2026-09-13T14:00:45Z
started: 2026-09-13T14:00:16Z
closed: 2026-09-13T14:00:45Z
---

## Description

Add `core.SprintStatus` (`draft|upcoming|current|completed`) with a `Valid()` method next to `SprintState` in `internal/core/sprint.go:24-37`, and `func (s *Sprint) DerivedStatus(now time.Time) SprintStatus`: `draft` when either date is absent, `completed` when `state == SprintClosed` or `end < today`, `current` when `start <= today <= end`, `upcoming` otherwise. `now` comes from the caller — `internal/core` resolves no timezone and reads no clock. Expose the value on `SprintSummary` (`internal/core/sprintview.go:38-59`) and fill it in `SummarizeSprint` (`:8`).

## Acceptance Criteria

- [x] Each status is covered by a boundary test (the first day, the last day, the day after, and the closed-overrides-calendar case).
- [x] `SprintSummary` carries the derived status and nothing writes it to a file.
- [x] `go test -race ./internal/core/...` passes and `make wasm` builds.

## Notes

`state: closed` is checked **before** the dateless check, so a closed sprint with no dates derives `completed` rather than `draft`. That follows the story's "`state: closed` always derives `completed`" over the ordering sketched in this description.

Also landed here, as the core seam for GIT-T-0107: `ParseSprintStatus`, `SprintStatusOrder`, `SprintStatus.Rank`, `FilterSprintsByStatus` and `SortSprintsForListing`.
