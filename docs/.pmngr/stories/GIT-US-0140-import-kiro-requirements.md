---
id: GIT-US-0140
type: story
title: Import Kiro requirements
status: backlog
priority: low
parent: GIT-EP-0030
author: claude
labels: [cli, core, agent-ok]
estimate: 3
created: 2026-09-24T12:12:14Z
updated: 2026-09-24T12:12:14Z
links:
  - { kind: blocked_by, target: GIT-US-0105 }
  - { kind: blocked_by, target: GIT-US-0106 }
  - { kind: blocked_by, target: GIT-US-0108 }
---

## Description

As a team moving from Kiro, I want `.kiro/specs/*/requirements.md` (user stories with EARS acceptance criteria) imported as specs with requirement blocks, and `tasks.md` optionally as tasks.

## Acceptance Criteria

- [ ] `gintrack spec import kiro <dir>` creates one spec per Kiro spec and one block per EARS criterion, keeping the Kiro numbering as a source reference.
- [ ] Optional `--tasks` imports `tasks.md` checkboxes as tasks linked with `implements`.
- [ ] Idempotent re-import; fixture tests; docs/07 updated.

## Notes

Deferred epic; no milestone.
