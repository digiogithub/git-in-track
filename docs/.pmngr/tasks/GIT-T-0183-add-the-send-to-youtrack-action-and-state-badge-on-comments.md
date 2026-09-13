---
id: GIT-T-0183
type: task
title: Add the Send to YouTrack action and state badge on comments
status: todo
priority: medium
parent: GIT-US-0076
milestone: GIT-M-0011
author: mcp
labels: [web, agent-ok]
estimate: 3
created: 2026-09-13T13:19:41Z
updated: 2026-09-13T13:19:41Z
---

## Description

Extend `CommentsPanel` in `web/src/features/backlog/ItemDetail.tsx:175-265` with a per-comment "Send to YouTrack" action and a state badge for pending, sent (with a link to the remote comment) and failed (with a retry). The action renders only when `capabilities.features.youtrack` is true, the project is linked and the item carries a YouTrack `external` reference; in `auto` mode the action collapses into the badge alone.

## Acceptance Criteria

- [ ] The action and badge render under the documented gating and disappear in browser-only mode.
- [ ] Pending, sent and failed states render with a link and a retry where applicable, and survive a reload.
- [ ] Auto mode hides the action and keeps the badge.
- [ ] Vitest covers gating, all three states and retry; `npm run tokens:check` passes.
