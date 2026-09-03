---
id: GIT-M-0004
type: milestone
title: Phase 3 — Team repository and boards
status: backlog
created: 2026-09-03T00:00:00Z
updated: 2026-09-03T00:00:00Z
author: team
labels: [core, web]
links:
  - kind: relates_to
    target: GIT-EP-0004
custom:
  phase: 3
  version: v0.4.0
---

## Description

Phase 3 of the roadmap. Multi-project workspaces driven by team.yaml, Kanban and Scrum
boards that reference items across project repositories, sprints, and read-only remote
references for projects that are not cloned locally.

## Acceptance Criteria

- [ ] A board shows cards from a cloned project and a remote-only project.
- [ ] A drag writes exactly one item status and the board `order:` list.
- [ ] Concurrent card moves produce a mergeable YAML diff.
- [ ] WIP limits enforced visually and never silently exceeded.
- [ ] Scrum board scopes to the active sprint with goal and dates.

## Notes

Epic: GIT-EP-0004. Stories: GIT-US-0016 … GIT-US-0019. Risk: R5.
