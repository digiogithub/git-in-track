---
id: GIT-T-0120
type: task
title: Implement BuildSprintSnapshot over the existing view and metrics types
status: done
priority: medium
parent: GIT-US-0080
milestone: GIT-M-0012
author: mcp
labels: [core]
estimate: 4
created: 2026-09-13T13:18:15Z
updated: 2026-09-13T14:01:16Z
started: 2026-09-13T14:00:56Z
closed: 2026-09-13T14:01:16Z
---

## Description

Add `func BuildSprintSnapshot(...)` to a new `internal/core/sprintsnapshot.go`. It aggregates totals from `SprintMetrics` (`internal/core/sprintview.go:16-35`), per-status, per-assignee and per-label distributions from the cards in `SprintView` (`:63-72`), and copies the burndown series and its `MetricsProvenance` (`internal/core/metrics.go:41-64`) verbatim. It is pure, takes `now` from the caller, caps the burndown at one point per sprint day, and never reaches for a clock or a filesystem.

## Acceptance Criteria

- [x] Totals, per-status, per-assignee and per-label counts match what the live views report for the same fixture.
- [x] The provenance is carried through, and a snapshot built from a non-git source is marked `approximate`.
- [x] Unresolved references are reported, never counted as done or as points.
- [x] `go test -race ./internal/core/...` covers the arithmetic and the burndown cap.

## Notes

**Signature deviation.** The sketch was `BuildSprintSnapshot(s, view, metrics SprintMetrics, burndown Burndown, now)`. What landed is `BuildSprintSnapshot(s *Sprint, view SprintView, metrics SprintMetricsView, now time.Time) SprintSnapshot`. Reason: `SprintMetrics` is already `view.Sprint.Metrics`, and `Burndown` carries no `MetricsProvenance`, so the sketched signature could not satisfy the "carries the provenance it froze" criterion without adding a field to `Burndown` in `metrics.go`. `SprintMetricsView` is what a caller already holds from `BuildSprintMetrics` and carries both.

Also landed: `SprintMetricsFromSnapshot(s, summary) (SprintMetricsView, bool)` — the read-side seam GIT-T-0129 needs — and `MetricsSourceSnapshot` / `SnapshotProvenance`, which word the "frozen at close" note. Only observed burndown days are frozen; a future day carries no measurement.
