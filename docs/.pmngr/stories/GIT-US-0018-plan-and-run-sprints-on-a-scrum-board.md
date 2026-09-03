---
id: GIT-US-0018
type: story
title: Plan and run sprints on a Scrum board
status: backlog
created: 2026-09-03T00:00:00Z
updated: 2026-09-03T00:00:00Z
author: team
priority: high
parent: GIT-EP-0004
milestone: GIT-M-0004
estimate: 8
labels: [web, core]
links:
  - kind: blocked_by
    target: GIT-US-0017
---

## Description

As a Scrum team, we want sprints with a goal, dates and a committed set of items, so that we
can plan and review iterations in the same files as everything else.

A sprint is `.pmngr/sprints/<id>.md` with `board`, `start`, `end`, `goal` and `items[]`
references. A board with `kind: scrum` scopes to the active sprint, shows the goal, the
remaining days and the committed points, and offers a planning view where items are moved
between the backlog and the sprint.

Closing a sprint reports what was completed, moves unfinished items to the next sprint or
back to the backlog by explicit choice, and leaves an auditable record in git.

## Acceptance Criteria

- [ ] A sprint file is created with id, board, dates, goal and item references.
- [ ] A Scrum board shows only the active sprint's items, with the goal and dates.
- [ ] Planning view moves items in and out of the sprint and shows committed points.
- [ ] Overlapping sprints on the same board are rejected with a clear message.
- [ ] Closing a sprint summarises completed versus incomplete work.
- [ ] Unfinished items are carried over or returned to the backlog by explicit choice.
- [ ] Sprint changes are one file write per affected file and merge cleanly.
- [ ] Past sprints remain browsable and are the input for metrics (GIT-US-0028).
