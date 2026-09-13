---
id: GIT-T-0157
type: task
title: Extend the sprint DataProvider methods with status, transfer and dry run
status: todo
priority: medium
parent: GIT-US-0089
milestone: GIT-M-0012
author: mcp
labels: [web, agent-ok]
estimate: 3
created: 2026-09-13T13:19:05Z
updated: 2026-09-13T13:19:05Z
---

## Description

Extend the sprint section of the `DataProvider` interface (`web/src/api/provider.ts:1023-1060`): sprint payload types gain the derived status and the optional snapshot, `closeSprint` gains `transfer` and `dryRun`, and a `transferSprintItems` method is added. Implement in the companion, browser and fake providers, and extend the query hooks in `web/src/features/boards/sprint-queries.ts` with the invalidation the `sprint.changed` event triggers.

## Acceptance Criteria

- [ ] All four implementations compile with no unimplemented method, and the sprint types carry derived status and snapshot.
- [ ] `closeSprint({dryRun: true})` returns the report without mutating any cache.
- [ ] Vitest against the fake provider covers the dry run, the real close and the transfer.
- [ ] `npm run lint` and `tsc` pass.
