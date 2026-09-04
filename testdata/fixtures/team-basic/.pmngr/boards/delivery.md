---
id: delivery
type: board
kind: kanban
title: Delivery
description: Everything the team is working on, across the Demo Shop and the website.
projects: [DEMO, WEB]
columns:
  - id: todo
    name: To Do
    categories: [todo]
    color: "#94a3b8"
  - id: in_progress
    name: In Progress
    statuses:
      "*": [in_progress]
      WEB: [doing]
    wip: 2
    color: "#2563eb"
  - id: in_review
    name: In Review
    statuses:
      "*": [in_review]
      WEB: [review]
    wip: 1
    color: "#a16207"
  - id: done
    name: Done
    categories: [done, cancelled]
    color: "#16a34a"
filters:
  types: [story, task]
card:
  show: [key, project, assignee, estimate, labels, due]
order:
  todo:
    - DEMO/DEMO-US-0002
    - WEB/WEB-US-0031
  in_progress:
    - DEMO/DEMO-US-0001
    - WEB/WEB-T-0007
  in_review:
    - DEMO/DEMO-T-0001
  done: []
created: 2026-07-01T09:00:00Z
updated: 2026-09-03T07:22:10Z
author: jose
---

## Notes

`DEMO` is cloned by the tests, so its cards are live and movable. `WEB` is
declared in [[../../team.yaml]] but never cloned, so `WEB/WEB-US-0031` and
`WEB/WEB-T-0007` render as remote cards: they keep their place in `order:` and
refuse to move until somebody clones the repository.
