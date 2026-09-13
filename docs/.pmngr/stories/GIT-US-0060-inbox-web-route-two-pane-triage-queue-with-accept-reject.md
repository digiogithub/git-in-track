---
id: GIT-US-0060
type: story
title: "Inbox web route: two-pane triage queue with accept, reject, snooze and duplicate"
status: done
priority: medium
parent: GIT-EP-0016
milestone: GIT-M-0012
assignees: [claude]
author: mcp
labels: [web]
estimate: 8
created: 2026-09-13T13:12:43Z
updated: 2026-09-13T17:08:54Z
started: 2026-09-13T16:27:29Z
closed: 2026-09-13T17:08:54Z
---

## Description

As a maintainer, I want an Inbox page that shows the triage queue on the left and the selected item on the right, with one-key accept, reject, snooze and duplicate and automatic movement to the next item, so that clearing a week of incoming work is a single focused pass instead of a navigation exercise.

The route is `/p/$project/inbox`, added with one `createRoute` in `web/src/app/router.tsx` (the whole tree lives in that file, `:23-148`) plus one sidebar entry in `web/src/app/layout/AppShell.tsx:38-53`, guarded so it only appears when the project's workflow declares a `triage` status. A new `web/src/features/inbox/` folder holds `InboxPage.tsx` (two-pane layout), `InboxList.tsx` (infinite query with "Load more", same shape as `features/backlog/ItemTable.tsx:112`), `InboxListItem.tsx`, `InboxDetail.tsx` (reusing `features/backlog/ItemBody.tsx` and the comments panel) and `InboxActions.tsx` (the action bar). Filters — `pending`, `snoozed`, `all` — are URL state through a zod schema next to `features/backlog/search.ts:58-73`, not component state, so a triage view is linkable.

Actions follow Plane's flow, which is the part worth copying: compute the next item **before** mutating and navigate after the mutation resolves, so accepting never leaves an empty pane. **Accept** opens the existing editor (`web/src/features/editor/ItemEditorPage.tsx:43`) retitled "Accept into the backlog" with the status field defaulted to the workflow's initial non-triage status; its save calls `triageInboxItem({action: 'accept', ...})` instead of a plain patch. **Reject** and **Snooze** (a date picker) are small dialogs; **Duplicate** uses `web/src/components/editor/ItemPicker.tsx:29-163` — the existing ARIA combobox — to choose the target. Every mutation goes through a new `DataProvider` method (`web/src/api/provider.ts`), implemented in the companion, browser, and fake providers, and is optimistic with rollback plus a pending-count badge update, as `features/boards/sprint-queries.ts` already does for sprints.

## Acceptance Criteria

- [x] `/p/$project/inbox` renders a two-pane triage view and the sidebar shows a pending-count badge; both are hidden for a project with no `triage` status.
- [x] The list supports `pending`, `snoozed` and `all` filters held in the URL, paginates incrementally, and a snoozed item whose date has passed appears under `pending`.
- [x] Accept opens the existing item edit form with a non-triage status preselected and commits the acceptance in the same save.
- [x] Reject, snooze (date picker) and duplicate (`ItemPicker` target) each have a dialog, report success and failure with a toast, and are refused cleanly on a stale rev with the existing `ConflictDialog`.
- [x] After any action the pane advances to the next queued item; next/previous buttons and keyboard shortcuts (`j`/`k`, `a`, `r`, `s`) do the same.
- [x] `listInbox`, `createInboxItem` and `triageInboxItem` exist on `DataProvider` and in all four implementations (interface, companion, browser, fake).
- [x] Optimistic updates roll back on error and the `inbox.changed` WS event refreshes an open pane.
- [x] Vitest covers the filter schema, the next-item computation, the optimistic rollback and the accept-form defaulting; `npm run lint` and `tsc` pass.

## Notes

Existing code: `web/src/app/router.tsx:18-187`, `web/src/app/layout/AppShell.tsx:68`, `web/src/features/backlog/search.ts:58-73` + `use-search.ts` (URL-as-filter), `web/src/features/backlog/ItemDetail.tsx` (detail pane and comments), `web/src/components/editor/ItemPicker.tsx` (the only typeahead in the app), `web/src/features/editor/ConflictDialog.tsx`, `web/src/api/provider.ts:6` (the four-provider rule, `docs/05 §4`).

Plane reference worth reading for the interaction only: `apps/web/core/components/inbox/content/inbox-issue-header.tsx` L126-160 (redirect-before-mutate) and `core/store/inbox/inbox-issue.store.ts` L99-142 (optimistic + badge arithmetic).

Do NOT call `fetch('/api/...')` from feature code — everything goes through `DataProvider`. Do NOT build a bespoke accept form; reuse the editor. Do NOT add `cmdk` or a new combobox library for the duplicate picker. Item bodies are untrusted: they must keep going through `web/src/markdown/sanitize.ts`.

The accept form reuses the editor's components through a wrapper rather than
`ItemEditorPage` itself; see the comment on this story for why and for the
follow-up that removes it.
