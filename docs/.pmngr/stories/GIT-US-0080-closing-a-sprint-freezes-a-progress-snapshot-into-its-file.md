---
id: GIT-US-0080
type: story
title: Closing a sprint freezes a progress snapshot into its file
status: done
priority: high
parent: GIT-EP-0017
milestone: GIT-M-0012
author: mcp
labels: [core, docs]
estimate: 8
created: 2026-09-13T13:14:15Z
updated: 2026-09-13T14:58:12Z
started: 2026-09-13T14:02:25Z
closed: 2026-09-13T14:58:12Z
---

## Description

As anyone looking back at a finished sprint, I want its numbers frozen into the sprint file at the moment it closed, so that velocity history stays readable years later from the Markdown alone — without a clone of every project, without walking git history, and unchanged by items that have since moved on.

ADR-017 says no time series is ever stored, because a burndown is a function of the item files and storing one would be a second truth to keep correct. Closing a sprint is the one moment where that reasoning inverts: after the close, the items leave the scope, so the numbers can no longer be recomputed from the current state at all. This story therefore adds a `snapshot` block to the sprint front matter, written exactly once by `sprint.close` and never recomputed: `{version, closed_at, totals: {items, resolved, done, unresolved, points, committed_points, done_points}, by_status: {<status>: n}, by_assignee: {<handle>: {total, done, points}}, by_label: {<label>: {total, done, points}}, burndown: [{date, remaining, remaining_points, ideal, completed, unknown}]}` — the same shape Plane freezes into `Cycle.progress_snapshot`, plus the provenance stanza this product owes its readers.

The computation is pure and lives in `internal/core`: `func BuildSprintSnapshot(s *Sprint, view SprintView, metrics SprintMetrics, burndown Burndown, now time.Time) SprintSnapshot`, fed by the existing `BuildSprintView` (`internal/core/sprintview.go:81`), `SummarizeSprint` (`:8`) and `BuildSprintMetrics` (`internal/core/metrics.go`). It carries the `MetricsProvenance` of the burndown it froze, so a snapshot taken in browser-only mode is honestly labelled `approximate` rather than passed off as reconstructed history. The read side then prefers the snapshot: `sprint.metrics` and the metrics REST/UI paths return the stored block when one is present and only walk git when it is not, and the UI says which it is showing. `version` starts at 1 so the format can evolve.

## Acceptance Criteria

- [ ] `core.SprintSnapshot` is parsed, validated and serialized as a `snapshot` block in the sprint front matter, with its own place in the key order of `SerializeSprint` (`internal/core/sprint.go:240`) and full round-tripping including unknown keys.
- [ ] `BuildSprintSnapshot` is pure, takes `now` from the caller, and produces totals, per-status, per-assignee, per-label and burndown series from the existing view and metrics types.
- [ ] The snapshot carries the `MetricsProvenance` of the data it froze; a snapshot built without git history is marked `approximate`.
- [ ] `sprint.close` writes the snapshot exactly once, in the same rev-checked write that sets `state: closed`; closing an already-closed sprint does not overwrite an existing snapshot.
- [ ] `sprint.metrics` and `GET /api/v1/sprints/{id}/burndown` return the stored snapshot when present, and say so in the provenance note, instead of walking history.
- [ ] `docs/04-team-repository.md` §8.2 documents the block and §12 documents the snapshot-wins rule; `docs/03-data-model.md` cross-references it.
- [ ] `docs/adr/ADR-034`'s sibling — the snapshot decision — is recorded as an amendment section referencing ADR-017 and explaining why a closed sprint is the one place a derived number is stored.
- [ ] `go test -race ./internal/core/...` covers snapshot arithmetic, round-tripping and the "do not overwrite" rule.

## Notes

Existing code: `internal/core/sprint.go:67-98` (`Sprint` + `sprintKnownKeys`), `:240` (`SerializeSprint`, key order), `internal/core/sprintview.go:16-59` (`SprintMetrics`, `SprintSummary`), `:205-258` (`SprintCloseReport`, `SummarizeClose`), `internal/core/metrics.go:24-90` (`MetricsSource`, `MetricsProvenance`, `ItemObservation`), `internal/vault/sprint.go:561-600` (`CloseSprint`), `internal/server/metrics.go`, `docs/adr/ADR-017-metrics-history-from-git-not-a-stored-time-series.md`.

Plane reference: `apps/api/plane/utils/cycle_transfer_issues.py` L280-432 (assignee and label distributions, burndown, then `save(update_fields=["progress_snapshot"])`) and `CycleProgressEndpoint` L712-718 (snapshot wins).

Do NOT store any series for an open sprint — ADR-017 still governs everything before the close. Do NOT recompute or "repair" an existing snapshot on later reads; it is a record of a moment, not a cache. Do NOT let the snapshot grow unbounded: cap the burndown at one point per sprint day.
