---
id: GIT-T-0165
type: task
title: Build the close-sprint dialog with a dry-run preview and target picker
status: done
priority: medium
parent: GIT-US-0089
milestone: GIT-M-0012
author: mcp
labels: [web]
estimate: 4
created: 2026-09-13T13:19:19Z
updated: 2026-09-13T16:39:07Z
started: 2026-09-13T16:38:02Z
closed: 2026-09-13T16:39:07Z
---

## Description

Add `web/src/features/boards/CloseSprintDialog.tsx`. On open it calls `closeSprint({dryRun: true})` and renders the report: finished, unfinished and unresolved counts, a destination choice (next sprint / backlog / leave), and — for "next sprint" — a picker listing only sprints of the same board whose derived status is not `completed`. Per-item refusals such as `repo_not_cloned` are listed before the confirm button is enabled, as an inline warning banner rather than a hidden action. Confirming re-runs without `dryRun` and reports the outcome with a toast.

## Acceptance Criteria

- [x] The dialog shows the dry-run counts, the destination choice and the filtered target picker.
- [x] Refusals are listed before confirming and the outcome is reported with success and error toasts.
- [x] Confirming closes the sprint and refreshes the board and sprint list.
- [x] Vitest covers the dry-run rendering, the target filtering and the refusal list.

## Notes

The preview is modelled as a *query* rather than a mutation, because a dry run
is a read that happens to compute a report: it is keyed by the chosen
destination, so changing the destination re-runs it, and it never writes to the
sprint cache.

The refusals gate the confirm button through an acknowledgement checkbox, which
resets whenever a new preview arrives — reading "this project is not cloned, so
these three items will not move" is the point of the preview, and a button that
is merely next to the banner is too easy to click past.
