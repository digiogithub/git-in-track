---
id: GIT-T-0055
type: task
title: Build the Inbox route, queue list and detail pane
status: done
priority: medium
parent: GIT-US-0060
milestone: GIT-M-0012
author: mcp
labels: [web]
estimate: 4
created: 2026-09-13T13:16:40Z
updated: 2026-09-13T16:39:56Z
started: 2026-09-13T16:39:43Z
closed: 2026-09-13T16:39:56Z
---

## Description

Add the `/p/$project/inbox` route with one `createRoute` in `web/src/app/router.tsx` plus a sidebar entry in `web/src/app/layout/AppShell.tsx:38-53`, both hidden when the project declares no triage status. Create `web/src/features/inbox/` with `InboxPage.tsx` (two-pane layout), `InboxList.tsx` (infinite query with "Load more"), `InboxListItem.tsx` and `InboxDetail.tsx`, reusing `features/backlog/ItemBody.tsx` and the comments panel from `features/backlog/ItemDetail.tsx`. Filters (`pending`, `snoozed`, `all`) are URL state through a zod schema written next to `features/backlog/search.ts:58-73`.

## Acceptance Criteria

- [x] The route renders a two-pane view, paginates incrementally and keeps its filter in the URL so a view is linkable.
- [x] The sidebar shows a pending count and both the route and the entry disappear for a project with no triage status.
- [x] Item bodies render through `web/src/markdown/sanitize.ts`.
- [x] Vitest covers the filter schema and the empty state.

## Notes

The detail pane's comment thread is read-only: `CommentsPanel` is not exported
from `ItemDetail.tsx`, so triage shows the thread and accepting opens the item,
where commenting lives. Exporting that panel is a small follow-up if a triager
turns out to want to reply in place.

`web/src/test/router.tsx` has no `/p/$project/inbox` route, so the sidebar entry
is covered by its own test tree rather than through `AppShell.test.tsx`. Adding
the route to the shared harness is a one-line follow-up.
