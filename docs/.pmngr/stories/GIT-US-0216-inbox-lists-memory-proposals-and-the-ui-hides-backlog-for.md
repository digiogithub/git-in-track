---
id: GIT-US-0216
type: story
title: Inbox lists memory proposals and the UI hides backlog for knowledge projects
status: done
priority: medium
parent: GIT-EP-0034
author: claude-code
assignees: [claude-code]
labels: [web, agent-ok]
estimate: 3
created: 2026-11-08T17:59:20Z
updated: 2026-11-09T13:00:00Z
links:
  - { kind: blocked_by, target: GIT-US-0210 }
---

## Description

Reuse the existing inbox (`web/src/features/inbox`) to review memory proposals (accept, reject, edit) and hide backlog, boards, sprints and specs for a `kind: knowledge` project. No new screens.

## Acceptance Criteria

- [x] A proposed memory shows in the inbox with its source and author; accepting moves it to `active`.
- [x] Knowledge projects show the KB and memory only.
- [x] Vitest coverage for both.

## Notes

Part of the gintrack hub PoC; see ADR-043 and ADR-044.
