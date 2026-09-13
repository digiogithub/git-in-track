---
id: GIT-T-0157
type: task
title: Extend the sprint DataProvider methods with status, transfer and dry run
status: done
priority: medium
parent: GIT-US-0089
milestone: GIT-M-0012
author: mcp
labels: [web, agent-ok]
estimate: 3
created: 2026-09-13T13:19:05Z
updated: 2026-09-13T16:27:16Z
started: 2026-09-13T16:27:03Z
closed: 2026-09-13T16:27:16Z
---

## Description

Extend the sprint section of the `DataProvider` interface (`web/src/api/provider.ts:1023-1060`): sprint payload types gain the derived status and the optional snapshot, `closeSprint` gains `transfer` and `dryRun`, and a `transferSprintItems` method is added. Implement in the companion, browser and fake providers, and extend the query hooks in `web/src/features/boards/sprint-queries.ts` with the invalidation the `sprint.changed` event triggers.

## Acceptance Criteria

- [x] All four implementations compile with no unimplemented method, and the sprint types carry derived status and snapshot.
- [x] `closeSprint({dryRun: true})` returns the report without mutating any cache.
- [x] Vitest against the fake provider covers the dry run, the real close and the transfer.
- [x] `npm run lint` and `tsc` pass.

## Notes

`closeSprint` now takes an options object (`SprintCloseInput`) rather than a
positional `carry`, because three of the four arguments had become optional and
`dryRun` had no natural place among them. `SprintResult.dryRun` marks a preview
so that no caller can mistake one for a commitment, and `useCloseSprint` /
`useTransferSprintItems` deliberately skip cache invalidation on a dry run: it
wrote nothing, and invalidating would suggest otherwise.

`SprintSummary.status` is the status derived by the core from the dates
(ADR-034) and is never recomputed in the front end. `MetricsSource` gained
`snapshot`; a snapshot-backed answer carries an empty `flow` and zeroed `stats`
because the cumulative-flow series is not frozen at the close.
