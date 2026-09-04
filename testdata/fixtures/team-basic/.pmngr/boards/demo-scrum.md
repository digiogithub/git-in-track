---
id: demo-scrum
type: board
kind: scrum
title: Demo Shop — Sprint Board
description: The sprint the Demo Delivery Team is running, across the shop and the website.
projects: [DEMO, WEB]
sprint: DEMO-TEAM-S-0001
backlog_column: sprint_backlog
columns:
  - id: sprint_backlog
    name: Sprint Backlog
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
    wip: 2
    color: "#a16207"
  - id: done
    name: Done
    categories: [done, cancelled]
    color: "#16a34a"
filters:
  types: [story, task]
card:
  show: [key, project, assignee, estimate]
order:
  in_progress:
    - DEMO/DEMO-US-0001
  in_review:
    - DEMO/DEMO-T-0001
created: 2026-08-24T09:00:00Z
updated: 2026-09-03T07:30:00Z
author: jose
---

## Notes

The board is scoped to [[../sprints/DEMO-TEAM-S-0001]]: only the references that
sprint lists appear in the working columns. `sprint_backlog` doubles as the
candidate drawer, so `DEMO/DEMO-US-0002` shows up there until somebody drags it
into the sprint.

`WEB/WEB-US-0031` is in the sprint even though nobody cloned the website: sprint
membership is team-repository state, so it stays editable while the card itself
stays read-only.
