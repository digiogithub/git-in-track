---
id: GIT-US-0138
type: story
title: Import Spec Kit specs
status: backlog
priority: low
parent: GIT-EP-0030
author: claude
labels: [cli, core, agent-ok]
estimate: 5
created: 2026-09-24T12:12:14Z
updated: 2026-09-24T12:12:14Z
links:
  - { kind: blocked_by, target: GIT-US-0105 }
  - { kind: blocked_by, target: GIT-US-0108 }
---

## Description

As a team moving from GitHub Spec Kit, I want `specs/<feature>/spec.md` functional requirements (`FR-NNN`) and acceptance scenarios imported as `spec` items with requirement blocks.

## Acceptance Criteria

- [ ] `gintrack spec import speckit <dir>` creates one spec per feature and one block per `FR-NNN`, converting Given/When/Then scenarios to `#### Scenario:` WHEN/THEN.
- [ ] The source identifier is kept (e.g. in the block body) and re-import is idempotent (updates, never duplicates, never renumbers).
- [ ] Output passes the grammar linter or reports warnings per block; fixture tests; docs/07 documents the command.

## Notes

Deferred epic; no milestone.
