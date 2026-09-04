---
id: GIT-US-0017
type: story
title: Run a Kanban board with drag and drop
status: in_review
created: 2026-09-03T00:00:00Z
updated: 2026-09-04
author: team
priority: critical
parent: GIT-EP-0004
milestone: GIT-M-0004
estimate: 8
labels: [web, core]
links:
  - kind: blocked_by
    target: GIT-US-0016
---

## Description

As a team, we want a Kanban board over items from all our projects, so that we can run our
delivery process without leaving the repository or duplicating our backlog into a SaaS
tool.

A board is `.pmngr/boards/<slug>.md` with `type: board`, `kind: kanban`, columns mapping to
statuses, WIP limits, filters (projects, labels) and card order stored as one ref per line
under `order:`. Dragging a card with dnd-kit writes the new status to the item file in its
own project repository and updates the board's order list — nothing else.

Keeping the order list one-ref-per-line is deliberate: it makes concurrent reordering
produce readable, mergeable diffs (risk R5).

## Acceptance Criteria

- [x] A board file renders as columns with cards from every referenced project.
- [x] Dragging between columns updates exactly one item's `status` and the board `order:`.
- [x] Reordering within a column updates only the board file.
- [x] WIP limits are shown and a violation is blocked with an explanation.
- [x] Board filters by project and label are applied on load.
- [x] Cards show id, title, assignees, labels, priority and estimate.
- [x] Drag and drop has a full keyboard alternative and announces changes to screen
      readers.
- [x] Two concurrent moves of different cards merge without a conflict, verified by a test.
- [x] An item whose status no longer maps to any column is surfaced, never hidden.

## Notes

Implemented across `internal/core` (board model, validation, view building, move
semantics, byte-stable emission), `internal/vault` (`board.list`, `board.get`,
`board.move` on the workspace), `internal/server` (the three REST routes) and
`web/src/features/boards` (dnd-kit board, keyboard alternative, WIP
confirmation, filters).

Decisions recorded in [ADR-013](../../adr/ADR-013-board-card-ordering.md): card
order stays a plain one-ref-per-line list; a fractional index was rejected.

The WIP rule reads as a contradiction between this story ("a violation is
blocked with an explanation") and doc 04 R-COL-5 ("advisory, never blocks"). It
is resolved the way doc 05 §9 already described: the move is refused **once**
with `wip_limit_exceeded` and the column and limit named, and a confirmation
repeats it with `force`. Never silently exceeded, never permanently blocked.

Out of scope and left to the next stories: scrum boards and sprint scoping
(GIT-US-0018), and a remote card's title and status from a committed snapshot
(GIT-US-0019) — a remote card currently shows its reference, a "remote" badge
and the reason it cannot be moved. `gintrack board` (doc 07 §4.6) is not part
of this story either.
