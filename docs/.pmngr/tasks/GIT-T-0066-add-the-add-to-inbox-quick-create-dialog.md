---
id: GIT-T-0066
type: task
title: Add the Add to inbox quick-create dialog
status: done
priority: medium
parent: GIT-US-0066
milestone: GIT-M-0012
author: mcp
labels: [web, agent-ok]
estimate: 2
created: 2026-09-13T13:16:57Z
updated: 2026-09-13T17:23:03Z
started: 2026-09-13T17:22:45Z
closed: 2026-09-13T17:23:03Z
---

## Description

Add an "Add to inbox" entry next to the existing new-item affordances (`web/src/features/backlog/NewItemLink.tsx` and the AppShell header) opening a dialog with a title field and an optional body, which calls `createInboxItem` and then links to the Inbox page. It is hidden for a project that declares no triage status, and it is the only capture form in the app that asks no type, parent or status question.

## Acceptance Criteria

- [x] The dialog creates a pending inbox item with `source: web` and shows a link to the queue.
- [x] The entry is hidden when the project has no triage status.
- [x] Vitest covers submit, validation of an empty title and the hidden case.

## Notes

Landed as `web/src/features/inbox/AddToInboxButton.tsx` (the trigger plus `AddToInboxDialog`, exported for a screen that owns its own trigger) and `useCreateInboxItem` in `web/src/features/inbox/queries.ts`. Until this change nothing in `web/` called `createInboxItem` at all: the provider method existed in all four providers and no screen offered it, so the inbox could be filled from the CLI and from an agent but not from the application.

**Placement deviates from the description, deliberately.** The control sits in the items page header beside `NewItemLink` (`features/backlog/ItemTable.tsx`) and in the Inbox page header, not in the AppShell header. The AppShell header is workspace-level and carries no project, so a global entry point would have had to open with a project picker — a planning question, which is the one thing this form exists not to ask. The sidebar already carries the per-project inbox entry, and that is where a project context exists. No acceptance criterion is affected.

The submission is `{project, type: 'story', title, body?, source: 'web'}` and nothing else: no status, no parent, no type picker. On success the dialog swaps to a confirmation naming the allocated id and linking to `/p/$project/inbox`, rather than closing silently — "somebody will look at this" is the only thing the person submitting wants confirmed. The badge is not moved optimistically; the `inbox.changed` event the host raises in both runtimes corrects it.

Hiding reuses `hasTriageStatus` on `useProject`, the same gate `InboxNavLink` uses, so the two cannot disagree about whether a project has an inbox.

`web/src/features/inbox/AddToInboxButton.test.tsx` covers the three cases: a submit that asserts the draft carries `source: web` and no `parent`/`status`, that the filed item comes back `pending`, and that the confirmation links to `/p/ACME/inbox`; an empty and a whitespace-only title keeping the submit button disabled with no provider call; and a project with no triage status rendering no control at all. `docs/05-web-app.md` §3.1 documents the form.
