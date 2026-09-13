---
id: GIT-T-0165
type: task
title: Build the close-sprint dialog with a dry-run preview and target picker
status: todo
priority: medium
parent: GIT-US-0089
milestone: GIT-M-0012
author: mcp
labels: [web]
estimate: 4
created: 2026-09-13T13:19:19Z
updated: 2026-09-13T13:19:19Z
---

## Description

Add `web/src/features/boards/CloseSprintDialog.tsx`. On open it calls `closeSprint({dryRun: true})` and renders the report: finished, unfinished and unresolved counts, a destination choice (next sprint / backlog / leave), and — for "next sprint" — a picker listing only sprints of the same board whose derived status is not `completed`. Per-item refusals such as `repo_not_cloned` are listed before the confirm button is enabled, as an inline warning banner rather than a hidden action. Confirming re-runs without `dryRun` and reports the outcome with a toast.

## Acceptance Criteria

- [ ] The dialog shows the dry-run counts, the destination choice and the filtered target picker.
- [ ] Refusals are listed before confirming and the outcome is reported with success and error toasts.
- [ ] Confirming closes the sprint and refreshes the board and sprint list.
- [ ] Vitest covers the dry-run rendering, the target filtering and the refusal list.
