---
id: GIT-EP-0004
type: epic
title: Team repository and boards
status: done
priority: high
milestone: GIT-M-0004
author: team
labels: [core, web, server]
estimate: 26
created: 2026-09-03T00:00:00Z
updated: 2026-09-06T00:00:00Z
closed: 2026-09-06T00:00:00Z
links:
  - { kind: blocked_by, target: GIT-EP-0003 }
---

## Description

Phase 3. Make the tool work for a team rather than one person. A team repository holds
`team.yaml` (members and the list of project repositories), a team-wide knowledge base, and
`.pmngr/boards/`, `.pmngr/sprints/`, `.pmngr/retros/`.

Boards reference items in project repositories (`ref: <projectKey>/<itemId>`) and never
copy them. Projects the user has not cloned appear as read-only remote references rendered
from the index snapshot committed under `.pmngr/index/<projectKey>.json`.

## Acceptance Criteria

- [x] A board in `testdata/fixtures/team-basic` shows cards from two projects, one cloned
      and one remote-only.
- [x] Dragging a card rewrites exactly one item file's `status` and the board's `order:`
      list, and nothing else.
- [x] Two people moving different cards produce a mergeable YAML diff.
- [x] WIP limits are enforced visually and cannot be exceeded silently.
- [x] Scrum boards scope to the active sprint and show its goal and dates.

## Notes

Stories: GIT-US-0016 … GIT-US-0019. See docs/11-roadmap.md, milestone 4.
Primary risk: R5 (YAML merge conflicts in board order lists).
