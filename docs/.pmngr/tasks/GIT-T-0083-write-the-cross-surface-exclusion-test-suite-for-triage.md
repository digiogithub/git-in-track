---
id: GIT-T-0083
type: task
title: Write the cross-surface exclusion test suite for triage items
status: done
priority: medium
parent: GIT-US-0071
milestone: GIT-M-0012
author: mcp
labels: [core]
estimate: 3
created: 2026-09-13T13:17:22Z
updated: 2026-09-13T16:19:12Z
started: 2026-09-13T16:18:54Z
closed: 2026-09-13T16:19:12Z
---

## Description

Add a Go test that builds one fixture project declaring a `triage` status, populates it with items in every inbox status plus ordinary backlog items, and asserts that `Index.Query` with a default filter, `BuildBoardView`, `BuildSprintView`, `SummarizeSprint`, the sprint candidate drawer, `BuildSprintMetrics` and `Index.Search` all return byte-identical results with and without the triage items present. Drive it table-wise so a future surface is one line to add.

## Acceptance Criteria

- [ ] The suite covers all seven surfaces and fails loudly if any one of them leaks a triage item.
- [x] The fixture is shared, not duplicated per test.
- [x] `go test -race ./internal/core/...` passes.

## Notes

Landed as `internal/core/triageexclusion_test.go`. One shared fixture
(`newTriageWorld`) is built twice per run — with and without the triage items —
and a table asks each surface the same question over both worlds, comparing with
`reflect.DeepEqual`. Adding a surface is one row.

**Six of the seven surfaces are proven identical**: `Index.Items` with a default
filter, `BuildBoardView` (columns and unmapped), `BuildSprintView` resolved
cards, the sprint candidate drawer, `SummarizeSprint` (points, done, done
points, resolved) and `BuildSprintMetrics` (burndown scope and completed, flow
bands).

**The seventh, `Index.Search`, is deliberately left unticked: it does not
exclude triage items, and this suite pins that it does not.** `Index.Search`
takes no `Filter`, so excluding there would make an inbox item unfindable by
text from every surface at once — the quick switcher and the MCP `search_items`
tool included — which contradicts both ADR-033 ("an item is real from the moment
of submission") and GIT-T-0088 ("a triage item is still readable"). ADR-033's
Decision section scopes the unconditional exclusion to board views, sprint
views, sprint candidates and sprint metrics, and scopes queries to a *default*
a caller can lift with `Filter.Inbox`; it says nothing about search. A search hit
carries no estimate, no status category and no column, so nothing reaches a
planning number through it. `TestSearchStillFindsATriageItem` records that
reasoning and will fail loudly if the behaviour changes without ADR-033 changing
first.

Two cases beyond the table, because they are the ones a reader would not guess:
`TestTriageItemIsExcludedEvenWhenAColumnAsksForIt` (a board column naming the
`triage` status, or declaring the `triage` category, still renders empty — the
exclusion happens in `walkCards` before any column is consulted, so a board
cannot opt back in) and `TestBurndownOverAFullInboxIsUnmoved` (100 triage items
of 13 points each, all named by the sprint file, move no burndown point).
