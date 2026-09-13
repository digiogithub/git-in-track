---
id: GIT-T-0216
type: task
title: Add the KB sync provider methods and status hook
status: done
priority: medium
parent: GIT-US-0093
milestone: GIT-M-0011
author: mcp
labels: [web, agent-ok]
estimate: 2
created: 2026-09-13T13:21:26Z
updated: 2026-09-13T16:34:09Z
started: 2026-09-13T16:33:58Z
closed: 2026-09-13T16:34:09Z
---

## Description

Add `kbSyncStatus`, `publishKbPage` and `pullKbPage` to the provider interface in `web/src/api/provider.ts` and to every implementation, with the browser-only one throwing a typed "companion required" error. Add `useKbSyncStatus` to `web/src/features/kb/useKbData.ts`, invalidated by the `sync.job.*` and `youtrack.kb.conflict` events.

## Acceptance Criteria

- [x] The three methods exist on every provider and fail clearly in browser-only mode.
- [x] `useKbSyncStatus` invalidates on both event families without a manual refresh.
- [x] Vitest covers the hook and the browser-only failure with a mocked provider.

## Notes

`ChangeEvent` gained a `kbConflict` member so the conflict frame reaches the
viewer with its `conflictPath`, which a plain status read cannot supply.

`remote` is a separate cache key rather than a parameter folded into one:
"in sync as far as this clone knows" and "in sync, verified against the
article" are different facts, and a tree-wide badge must never cost one request
per page.

The `sync.job.*` invalidation is filtered to the `youtrack.kb.*` kinds, so a
long import no longer refetches every page's sync state.
