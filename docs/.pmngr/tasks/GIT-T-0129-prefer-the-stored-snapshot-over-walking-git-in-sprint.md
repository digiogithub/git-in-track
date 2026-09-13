---
id: GIT-T-0129
type: task
title: Prefer the stored snapshot over walking git in sprint metrics
status: done
priority: medium
parent: GIT-US-0080
milestone: GIT-M-0012
author: mcp
labels: [core, server]
estimate: 3
created: 2026-09-13T13:18:29Z
updated: 2026-09-13T14:57:58Z
started: 2026-09-13T14:57:45Z
closed: 2026-09-13T14:57:58Z
---

## Description

Make `sprint.metrics` (`internal/vault/sprint.go`, dispatch at `internal/vault/dispatch.go:179`) and `GET /api/v1/sprints/{id}/burndown` (`internal/server/metrics.go` / `sprints.go`) return the stored snapshot when the sprint carries one, without touching the git history walk, and set a provenance note saying the numbers were frozen at close. Only a sprint without a snapshot falls through to `BuildSprintMetrics` and the `HistorySource`.

## Acceptance Criteria

- [x] A closed sprint with a snapshot answers from it and never invokes the history source (asserted with a fake that fails if called).
- [x] The provenance note distinguishes "frozen at close" from live reconstruction.
- [x] An open sprint behaves exactly as before.
- [x] `go test -race ./internal/vault/...` covers both paths.

## Notes

`Workspace.SprintMetrics` calls `core.SprintMetricsFromSnapshot(sprint, view.Sprint)` before any history is gathered and returns its result when it reports true; everything else is unchanged. Covered by `TestSprintCloseFreezesTheSnapshot` in `internal/vault/metrics_test.go`, whose `refusingHistory` source fails the test if it is asked.

The `GET /api/v1/sprints/{id}/burndown` half is served by the same vault call, so it inherits the behaviour; the server agent should note that a snapshot-backed answer carries an empty `flow` and `stats` (the cumulative flow diagram is not frozen) and should either omit those panels or say they are unavailable. The docs/04 R-MET-12 "As built" paragraph was deleted.
