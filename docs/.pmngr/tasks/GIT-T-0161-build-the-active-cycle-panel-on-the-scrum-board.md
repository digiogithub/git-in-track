---
id: GIT-T-0161
type: task
title: Build the active cycle panel on the scrum board
status: done
priority: medium
parent: GIT-US-0089
milestone: GIT-M-0012
author: mcp
labels: [web]
estimate: 4
created: 2026-09-13T13:19:10Z
updated: 2026-09-13T16:37:59Z
started: 2026-09-13T16:37:46Z
closed: 2026-09-13T16:37:59Z
---

## Description

Add `web/src/features/boards/ActiveCycle.tsx`, rendered above the columns in `BoardView.tsx` for a scrum board: goal, date range, days remaining, a progress bar of done against committed points, the added-mid-sprint count and a compact burndown reusing the components in `web/src/features/metrics/`. The burndown comes from the stored snapshot when the sprint carries one and live otherwise, and `provenance.note` is always printed above the chart as ADR-017 requires. A draft sprint shows its planning state instead of a progress bar.

## Acceptance Criteria

- [x] The panel renders for a scrum board and shows every listed figure, correct for a current, an upcoming and a draft sprint.
- [x] The provenance note is always visible above the chart, and a snapshot-backed chart says it was frozen at close.
- [x] No new charting dependency is added.
- [x] Vitest covers the three sprint states and the provenance rendering.

## Notes

The panel lives inside `SprintPanel`, which `BoardView` already renders above
the columns, rather than as a second card: one card reads as one object, and it
keeps the goal editor next to the goal. `BoardView.tsx` is unchanged.

A draft shows a planning state rather than a progress bar, because a bar at
zero reads as "no work done" instead of "not started yet".

`SprintMetrics` was corrected in the same pass: a snapshot-backed view no
longer renders the cumulative-flow and flow-statistics panels as zeros — they
say the series was not frozen at the close.
