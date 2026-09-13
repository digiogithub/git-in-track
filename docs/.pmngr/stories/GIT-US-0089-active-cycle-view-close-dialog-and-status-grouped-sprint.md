---
id: GIT-US-0089
type: story
title: Active cycle view, close dialog and status-grouped sprint list in the web app
status: backlog
priority: medium
parent: GIT-EP-0017
milestone: GIT-M-0012
author: mcp
labels: [web]
estimate: 8
created: 2026-09-13T13:14:59Z
updated: 2026-09-13T13:14:59Z
---

## Description

As a team working a scrum board, I want the running cycle summarised at the top of the board — progress, days left, burndown — and closing it to be a dialog that tells me exactly what is about to move where, so that I can see where the sprint stands without leaving the board and never close one by accident.

Three pieces, all inside `web/src/features/boards/`. First, an `ActiveCycle.tsx` panel rendered above the columns of a scrum board (`BoardView.tsx`): the goal, the date range, days remaining, a progress bar of done against committed points, the added-mid-sprint count, and a compact burndown fed by the existing metrics query — from the stored snapshot for a closed sprint, live otherwise, always with the `provenance.note` printed above the chart as ADR-017 requires. Second, `CloseSprintDialog.tsx`: it calls the close in `dryRun` mode first and renders the returned report — n finished, n unfinished, n unresolved — with a mode choice (next sprint / backlog / leave) and, for "next sprint", a target picker listing the board's non-completed sprints; refusals such as `repo_not_cloned` are shown per item before confirming, not after. Third, `SprintList.tsx` is regrouped by derived status — Current, Upcoming, Draft, Completed — with a "Draft" badge for a dateless sprint and a hint that adding dates is what schedules it.

`NewSprintDialog.tsx` follows: dates become optional, and a `sprint_overlap` refusal renders the message naming the other sprint together with the "remove the dates to keep it a draft" escape hatch. All data access goes through `DataProvider` (`web/src/api/provider.ts:1023-1060`), whose sprint methods gain the derived status, the transfer options and the dry-run flag, in all four implementations.

## Acceptance Criteria

- [ ] A scrum board shows an active-cycle panel with goal, dates, days remaining, progress against committed points, added-mid-sprint count and a burndown; the provenance note is printed above the chart.
- [ ] A closed sprint's panel and metrics read the stored snapshot and say so; an open one reads live metrics.
- [ ] The close dialog runs a dry run first and shows finished, unfinished and unresolved counts, the chosen destination, and every per-item refusal before the confirm button is enabled.
- [ ] The target picker lists only sprints of the same board whose derived status is not `completed`.
- [ ] `SprintList` groups by derived status in the order current → upcoming → draft → completed and marks drafts with a badge.
- [ ] `NewSprintDialog` accepts a sprint with no dates and renders a `sprint_overlap` refusal with the other sprint's name and range plus the draft escape hatch.
- [ ] `DataProvider` sprint methods carry derived status, transfer options and `dryRun`, implemented in the companion, browser and fake providers.
- [ ] Vitest covers the grouping, the dry-run rendering, the draft badge and the overlap error path; `npm run lint` and `tsc` pass.

## Notes

Existing code: `web/src/features/boards/BoardView.tsx`, `SprintPanel.tsx`, `SprintList.tsx`, `NewSprintDialog.tsx`, `sprint-queries.ts`, `snapshot-age.ts`; `web/src/api/provider.ts:823-843` (the `sprint_overlap` / `sprint_already_active` / `repo_not_cloned` error codes already exist), `:1023-1060` (sprint methods); metrics UI at `web/src/features/metrics/`; router at `web/src/app/router.tsx:101-135`.

Plane reference for the interaction only: `apps/web/core/components/cycles/active-cycle/`, `transfer-issues.tsx` L18-42 (the inline warning banner as the gate for the bulk action) and `transfer-issues-modal.tsx` L47-60 (success and error toasts).

Do NOT compute derived status in the front end — it comes from the core so both hosts agree. Do NOT render a burndown without its provenance note. Do NOT add a charting dependency if the existing metrics components can be reused.
