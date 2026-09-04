---
id: GIT-US-0017
type: story
title: Run a Kanban board with drag and drop
status: in_progress
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

- [ ] A board file renders as columns with cards from every referenced project.
- [ ] Dragging between columns updates exactly one item's `status` and the board `order:`.
- [ ] Reordering within a column updates only the board file.
- [ ] WIP limits are shown and a violation is blocked with an explanation.
- [ ] Board filters by project and label are applied on load.
- [ ] Cards show id, title, assignees, labels, priority and estimate.
- [ ] Drag and drop has a full keyboard alternative and announces changes to screen
      readers.
- [ ] Two concurrent moves of different cards merge without a conflict, verified by a test.
- [ ] An item whose status no longer maps to any column is surfaced, never hidden.
