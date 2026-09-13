---
id: GIT-T-0169
type: task
title: Raise background toasts for job completion and failure
status: done
priority: medium
parent: GIT-US-0081
milestone: GIT-M-0011
author: mcp
labels: [web, agent-ok]
estimate: 2
created: 2026-09-13T13:19:24Z
updated: 2026-09-13T15:49:39Z
started: 2026-09-13T15:49:26Z
closed: 2026-09-13T15:49:39Z
---

## Description

Subscribe to `sync.job.done` and `sync.job.failed` at the app shell level so a job finishing or failing raises a toast wherever the user is, not only on the Settings page. Coalesce a burst into one summary toast, and give the failure toast an action that navigates to the queue table.

## Acceptance Criteria

- [x] A job finishing or failing raises a toast from any route.
- [x] A burst of completions produces one summary toast rather than many.
- [x] The failure toast links to the queue table.
- [x] Vitest covers the toast, the coalescing and the navigation action.

## Notes

`web/src/features/sync/SyncJobToasts.tsx`, mounted in `AppShell` inside a new app-wide `ToastProvider` (the host used to be per route, which is why Settings had no toasts at all).

Terminal frames are gathered for 1 s and raised as one summary, failures counted apart from successes. That window is a *notification* concern and is unrelated to the engine's own 500 ms progress coalescing — progress frames are never read here.

A job that ends `cancelled` raises nothing: the person who cancelled it does not need to be told it stopped.

`ToastInput` gained an optional `action: {label, onClick}` for the "Show the queue" follow-up; it dismisses the toast when taken.
