---
id: GIT-T-0137
type: task
title: Bridge sync.job.* into TanStack Query with useSyncJobEvents
status: todo
priority: medium
parent: GIT-US-0074
milestone: GIT-M-0011
author: mcp
labels: [web, agent-ok]
estimate: 3
created: 2026-09-13T13:18:40Z
updated: 2026-09-13T13:18:40Z
---

## Description

Add a `useSyncJobEvents` hook subscribing to the `sync.job.*` topics over the existing event stream and invalidating the jobs query, following `useBacklogEvents` (`web/src/features/backlog/queries.ts:130-148`). It must unsubscribe on unmount and tolerate a reconnect, resuming with `since` as the existing client does.

## Acceptance Criteria

- [ ] The hook subscribes, invalidates the jobs query and unsubscribes cleanly on unmount.
- [ ] A reconnect resumes without losing the last known state.
- [ ] Vitest covers subscribe, invalidate, unmount and reconnect against a fake event source.
