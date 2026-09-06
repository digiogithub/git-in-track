---
id: platform
type: board
kind: kanban
title: Platform
description: The platform team's own view over the Demo Shop backlog.
projects: [DEMO]
columns:
  - id: todo
    name: To Do
    categories: [todo]
  - id: in_progress
    name: In Progress
    statuses:
      "*": [in_progress]
    wip: 3
  - id: done
    name: Done
    categories: [done, cancelled]
filters:
  types: [story, task]
order:
  todo:
    - DEMO/DEMO-US-0002
  in_progress:
    - DEMO/DEMO-US-0001
  done: []
created: 2026-08-01T09:00:00Z
updated: 2026-09-04T10:00:00Z
author: nuria
---

## Notes

This board belongs to a different team repository than `delivery`, and both are
open at once: the active team is what decides which of the two is listed.
