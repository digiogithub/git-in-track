---
id: GIT-T-0120
type: task
title: Implement BuildSprintSnapshot over the existing view and metrics types
status: todo
priority: medium
parent: GIT-US-0080
milestone: GIT-M-0012
author: mcp
labels: [core]
estimate: 4
created: 2026-09-13T13:18:15Z
updated: 2026-09-13T13:18:15Z
---

## Description

Add `func BuildSprintSnapshot(s *Sprint, view SprintView, metrics SprintMetrics, burndown Burndown, now time.Time) SprintSnapshot` to a new `internal/core/sprintsnapshot.go`. It aggregates totals from `SprintMetrics` (`internal/core/sprintview.go:16-35`), per-status, per-assignee and per-label distributions from the cards in `SprintView` (`:63-72`), and copies the burndown series and its `MetricsProvenance` (`internal/core/metrics.go:41-64`) verbatim. It is pure, takes `now` from the caller, caps the burndown at one point per sprint day, and never reaches for a clock or a filesystem.

## Acceptance Criteria

- [ ] Totals, per-status, per-assignee and per-label counts match what the live views report for the same fixture.
- [ ] The provenance is carried through, and a snapshot built from a non-git source is marked `approximate`.
- [ ] Unresolved references are reported, never counted as done or as points.
- [ ] `go test -race ./internal/core/...` covers the arithmetic and the burndown cap.
