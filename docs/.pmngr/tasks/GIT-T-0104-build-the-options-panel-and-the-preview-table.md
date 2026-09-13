---
id: GIT-T-0104
type: task
title: Build the options panel and the preview table
status: todo
priority: medium
parent: GIT-US-0059
milestone: GIT-M-0011
author: mcp
labels: [web, agent-ok]
estimate: 3
created: 2026-09-13T13:17:53Z
updated: 2026-09-13T13:17:53Z
---

## Description

Add `web/src/features/youtrack/ImportOptions.tsx` with the subtasks depth stepper and the linked-issues, comments and attachments checkboxes, plus a disabled "land in Inbox" checkbox whose tooltip points at the future Inbox epic. Add `ImportPreviewTable.tsx` rendering the preview result: one row per issue with create-or-update, mapped type, status, parent and any warnings, with warnings visually distinct and not blocking.

## Acceptance Criteria

- [ ] Options are sent unchanged to preview and run; the Inbox option is rendered disabled with an explanatory tooltip.
- [ ] The preview table shows action, mapped type, status, parent and warnings per issue.
- [ ] Warnings are visible but do not block running the import.
- [ ] Vitest covers option wiring and preview rendering including the warning row; `npm run tokens:check` passes.
