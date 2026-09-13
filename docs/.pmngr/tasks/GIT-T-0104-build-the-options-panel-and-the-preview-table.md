---
id: GIT-T-0104
type: task
title: Build the options panel and the preview table
status: done
priority: medium
parent: GIT-US-0059
milestone: GIT-M-0011
author: mcp
labels: [web, agent-ok]
estimate: 3
created: 2026-09-13T13:17:53Z
updated: 2026-09-13T15:48:14Z
started: 2026-09-13T15:48:01Z
closed: 2026-09-13T15:48:14Z
---

## Description

Add `web/src/features/youtrack/ImportOptions.tsx` with the subtasks depth stepper and the linked-issues, comments and attachments checkboxes, plus a disabled "land in Inbox" checkbox whose tooltip points at the future Inbox epic. Add `ImportPreviewTable.tsx` rendering the preview result: one row per issue with create-or-update, mapped type, status, parent and any warnings, with warnings visually distinct and not blocking.

## Acceptance Criteria

- [x] Options are sent unchanged to preview and run; the Inbox option is rendered disabled with an explanatory tooltip.
- [x] The preview table shows action, mapped type, status, parent and warnings per issue.
- [x] Warnings are visible but do not block running the import.
- [x] Vitest covers option wiring and preview rendering including the warning row; `npm run tokens:check` passes.

## Notes

The shared vocabulary (`IMPORT_PRESETS`, `IMPORT_MAX_DEPTH`, `ImportOptionsDraft`, `defaultImportOptions`) lives in `web/src/features/youtrack/import-model.ts`: three components use it, and a constant exported beside a component costs the dev server its fast refresh.

The plan row carries the mapped **type**, not a mapped status: `YouTrackImportPlanItem` (`internal/vault/youtrack.go:165`) has `mappedType`, `parent` and `milestone` and no status field, so the table renders type, parent and the warnings rather than inventing a column the operation does not answer.

A disabled input fires no pointer events, so the Inbox tooltip is attached to the row around it and the reason also reaches a keyboard user through `aria-describedby`.
