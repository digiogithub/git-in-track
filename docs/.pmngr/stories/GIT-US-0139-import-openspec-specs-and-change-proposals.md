---
id: GIT-US-0139
type: story
title: Import OpenSpec specs and change proposals
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
  - { kind: blocked_by, target: GIT-US-0109 }
---

## Description

As a team moving from OpenSpec, I want `openspec/specs/*/spec.md` (`### Requirement:` + `#### Scenario:`) imported as specs, and open `openspec/changes/*` proposals imported as stories with a `## Spec Delta`.

## Acceptance Criteria

- [ ] `gintrack spec import openspec <dir>` maps each capability to a spec and each requirement to a block, preserving scenarios.
- [ ] Change proposals become stories whose Spec Delta mirrors ADDED/MODIFIED/REMOVED.
- [ ] Idempotent re-import; fixture tests; docs/07 updated.

## Notes

Deferred epic; no milestone.
