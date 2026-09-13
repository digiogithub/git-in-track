---
id: GIT-T-0183
type: task
title: Add the Send to YouTrack action and state badge on comments
status: done
priority: medium
parent: GIT-US-0076
milestone: GIT-M-0011
author: mcp
labels: [web, agent-ok]
estimate: 3
created: 2026-09-13T13:19:41Z
updated: 2026-09-13T16:36:36Z
started: 2026-09-13T16:36:24Z
closed: 2026-09-13T16:36:36Z
---

## Description

Extend `CommentsPanel` in `web/src/features/backlog/ItemDetail.tsx:175-265` with a per-comment "Send to YouTrack" action and a state badge for pending, sent (with a link to the remote comment) and failed (with a retry). The action renders only when `capabilities.features.youtrack` is true, the project is linked and the item carries a YouTrack `external` reference; in `auto` mode the action collapses into the badge alone.

## Acceptance Criteria

- [x] The action and badge render under the documented gating and disappear in browser-only mode.
- [x] Pending, sent and failed states render with a link and a retry where applicable, and survive a reload.
- [x] Auto mode hides the action and keeps the badge.
- [x] Vitest covers gating, all three states and retry; `npm run tokens:check` passes.

## Notes

No delete-remotely action is offered, and a test asserts its absence:
deletion is deliberately local only, because a repository is not the authority
on an issue's conversation.

A push started elsewhere — an agent over MCP, another tab — reaches this panel
through the same derivation, since the state is read from `external` plus the
job stream rather than from anything this component remembers.
