---
id: GIT-T-0179
type: task
title: Add the comment push provider methods and hooks
status: todo
priority: medium
parent: GIT-US-0076
milestone: GIT-M-0011
author: mcp
labels: [web, agent-ok]
estimate: 2
created: 2026-09-13T13:19:37Z
updated: 2026-09-13T13:19:37Z
---

## Description

Add `pushCommentToYoutrack` to the provider interface in `web/src/api/provider.ts` and to every provider implementation, with the browser-only one throwing a typed "companion required" error, and add a mutation hook plus a `useCommentSyncState(comment)` selector in `web/src/features/backlog/queries.ts` that derives pending, sent or failed from the comment's `external` field and the `sync.job.*` events for its path.

## Acceptance Criteria

- [ ] The provider method exists everywhere and fails clearly in browser-only mode.
- [ ] `useCommentSyncState` derives state from `external` plus events, not from component state.
- [ ] Vitest covers the three derived states with mocked events and provider.
