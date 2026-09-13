---
id: GIT-EP-0017
type: epic
title: "Cycles: date-driven sprints with snapshots and transfer"
status: backlog
priority: medium
milestone: GIT-M-0012
author: mcp
labels: [core, web, server]
created: 2026-09-13T13:08:15Z
updated: 2026-09-13T13:08:15Z
---

## Description

Extend the existing sprint entity with what Plane's cycles do well. A sprint's status (`draft`, `upcoming`, `current`, `completed`) is derived from its dates, never stored; a sprint without dates is a draft; two sprints of one board cannot overlap; closing a sprint freezes a progress snapshot (totals, done, per-assignee, per-label, burndown points) into the sprint file so velocity history is readable from git without replaying it; unfinished items can be transferred to the next sprint in one action. The Scrum board gains an "active cycle" view with progress and the transfer dialog.

## Acceptance Criteria

- [ ] Sprint front matter: optional dates make a draft; `snapshot` block written on close; documented with an ADR (extends ADR-017 on metrics history).
- [ ] Core validation: no overlapping date ranges on one board; both dates or neither.
- [ ] Derived status exposed by the index, REST and MCP; sort and filter by it.
- [ ] Close sprint action: snapshot + transfer incomplete items to a chosen sprint (or backlog); confirmation with counts.
- [ ] Active cycle view on the board: progress bar, days left, burndown from snapshot or live.
- [ ] MCP `close_sprint`, `transfer_sprint_items`; CLI equivalents.

## Notes

Plane reference: `apps/api/plane/db/models/cycle.py`, `cycle_issue` transfer endpoint, `progress_snapshot`.
