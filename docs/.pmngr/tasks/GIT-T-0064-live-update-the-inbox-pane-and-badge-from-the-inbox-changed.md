---
id: GIT-T-0064
type: task
title: Live-update the inbox pane and badge from the inbox.changed event
status: todo
priority: medium
parent: GIT-US-0060
milestone: GIT-M-0012
author: mcp
labels: [web, agent-ok]
estimate: 2
created: 2026-09-13T13:16:53Z
updated: 2026-09-13T13:16:53Z
---

## Description

Subscribe the Inbox page and the sidebar badge to the `inbox.changed` WS topic through the existing events client in `web/src/api/`, invalidating the inbox query keys and updating the pending count without a full refetch of the open detail pane. Fall back cleanly in browser-only mode, where there is no WebSocket at all, by relying on query invalidation after each local mutation.

## Acceptance Criteria

- [ ] A triage performed elsewhere updates an open inbox list and the sidebar badge.
- [ ] Browser-only mode shows no error and still updates after local mutations.
- [ ] Vitest covers the event handler and the browser-mode fallback.
