---
id: GIT-T-0216
type: task
title: Add the KB sync provider methods and status hook
status: todo
priority: medium
parent: GIT-US-0093
milestone: GIT-M-0011
author: mcp
labels: [web, agent-ok]
estimate: 2
created: 2026-09-13T13:21:26Z
updated: 2026-09-13T13:21:26Z
---

## Description

Add `kbSyncStatus`, `publishKbPage` and `pullKbPage` to the provider interface in `web/src/api/provider.ts` and to every implementation, with the browser-only one throwing a typed "companion required" error. Add `useKbSyncStatus` to `web/src/features/kb/useKbData.ts`, invalidated by the `sync.job.*` and `youtrack.kb.conflict` events.

## Acceptance Criteria

- [ ] The three methods exist on every provider and fail clearly in browser-only mode.
- [ ] `useKbSyncStatus` invalidates on both event families without a manual refresh.
- [ ] Vitest covers the hook and the browser-only failure with a mocked provider.
