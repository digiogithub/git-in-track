---
id: GIT-T-0169
type: task
title: Raise background toasts for job completion and failure
status: todo
priority: medium
parent: GIT-US-0081
milestone: GIT-M-0011
author: mcp
labels: [web, agent-ok]
estimate: 2
created: 2026-09-13T13:19:24Z
updated: 2026-09-13T13:19:24Z
---

## Description

Subscribe to `sync.job.done` and `sync.job.failed` at the app shell level so a job finishing or failing raises a toast wherever the user is, not only on the Settings page. Coalesce a burst into one summary toast, and give the failure toast an action that navigates to the queue table.

## Acceptance Criteria

- [ ] A job finishing or failing raises a toast from any route.
- [ ] A burst of completions produces one summary toast rather than many.
- [ ] The failure toast links to the queue table.
- [ ] Vitest covers the toast, the coalescing and the navigation action.
