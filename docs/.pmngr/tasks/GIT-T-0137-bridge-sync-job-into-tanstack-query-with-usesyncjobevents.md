---
id: GIT-T-0137
type: task
title: Bridge sync.job.* into TanStack Query with useSyncJobEvents
status: done
priority: medium
parent: GIT-US-0074
milestone: GIT-M-0011
author: mcp
labels: [web, agent-ok]
estimate: 3
created: 2026-09-13T13:18:40Z
updated: 2026-09-13T15:49:56Z
started: 2026-09-13T15:49:43Z
closed: 2026-09-13T15:49:56Z
---

## Description

Add a `useSyncJobEvents` hook subscribing to the `sync.job.*` topics over the existing event stream and invalidating the jobs query, following `useBacklogEvents` (`web/src/features/backlog/queries.ts:130-148`). It must unsubscribe on unmount and tolerate a reconnect, resuming with `since` as the existing client does.

## Acceptance Criteria

- [x] The hook subscribes, invalidates the jobs query and unsubscribes cleanly on unmount.
- [x] A reconnect resumes without losing the last known state.
- [x] Vitest covers subscribe, invalidate, unmount and reconnect against a fake event source.

## Notes

`web/src/features/sync/queries.ts`. The subscription work is split the way the rest of the app splits it: `CompanionProvider` owns the socket and normalizes the five topics into one `ChangeEvent` variant (`{kind: 'syncJob', job}`), and the hook owns the cache.

Reconciliation is a synthetic **`resync` phase** rather than a special case in the hook. The provider raises it after a reconnect, on `stream.overflow` (now handled; it was not before) and on `resume.gap`, and it means "the live counts are no longer trustworthy, read `GET /api/v1/sync/jobs`". `resume` with the last `seq` still runs first, so a replay that is still in the ring is served normally.

The hook adds **no** throttling: progress is already coalesced to one frame per 500 ms per group and terminal frames are never throttled, so a second layer here would only delay the truth. `onEvent` is held in a ref so an inline closure does not resubscribe.
