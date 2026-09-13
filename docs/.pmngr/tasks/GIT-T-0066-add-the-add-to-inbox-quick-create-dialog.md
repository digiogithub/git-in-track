---
id: GIT-T-0066
type: task
title: Add the Add to inbox quick-create dialog
status: todo
priority: medium
parent: GIT-US-0066
milestone: GIT-M-0012
author: mcp
labels: [web, agent-ok]
estimate: 2
created: 2026-09-13T13:16:57Z
updated: 2026-09-13T13:16:57Z
---

## Description

Add an "Add to inbox" entry next to the existing new-item affordances (`web/src/features/backlog/NewItemLink.tsx` and the AppShell header) opening a dialog with a title field and an optional body, which calls `createInboxItem` and then links to the Inbox page. It is hidden for a project that declares no triage status, and it is the only capture form in the app that asks no type, parent or status question.

## Acceptance Criteria

- [ ] The dialog creates a pending inbox item with `source: web` and shows a link to the queue.
- [ ] The entry is hidden when the project has no triage status.
- [ ] Vitest covers submit, validation of an empty title and the hidden case.
