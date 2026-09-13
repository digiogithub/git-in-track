---
id: GIT-T-0179
type: task
title: Add the comment push provider methods and hooks
status: done
priority: medium
parent: GIT-US-0076
milestone: GIT-M-0011
author: mcp
labels: [web, agent-ok]
estimate: 2
created: 2026-09-13T13:19:37Z
updated: 2026-09-13T16:36:20Z
started: 2026-09-13T16:36:08Z
closed: 2026-09-13T16:36:20Z
---

## Description

Add `pushCommentToYoutrack` to the provider interface in `web/src/api/provider.ts` and to every provider implementation, with the browser-only one throwing a typed "companion required" error, and add a mutation hook plus a `useCommentSyncState(comment)` selector in `web/src/features/backlog/queries.ts` that derives pending, sent or failed from the comment's `external` field and the `sync.job.*` events for its path.

## Acceptance Criteria

- [x] The provider method exists everywhere and fails clearly in browser-only mode.
- [x] `useCommentSyncState` derives state from `external` plus events, not from component state.
- [x] Vitest covers the three derived states with mocked events and provider.

## Notes

`Comment` gained `external`, so a reload shows the truth: a comment carrying a
`youtrack` entry has arrived, whatever any event said.

`external` always wins over a job frame. A re-delivered `failed` frame cannot
un-send a comment that is already on the issue, which is the case the engine's
at-least-once delivery makes real rather than theoretical.

The queued job is recorded from the push *result*, which is the companion's
statement about what it queued, never from the click.
