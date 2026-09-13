---
id: GIT-T-0055
type: task
title: Build the Inbox route, queue list and detail pane
status: todo
priority: medium
parent: GIT-US-0060
milestone: GIT-M-0012
author: mcp
labels: [web]
estimate: 4
created: 2026-09-13T13:16:40Z
updated: 2026-09-13T13:16:40Z
---

## Description

Add the `/p/$project/inbox` route with one `createRoute` in `web/src/app/router.tsx` plus a sidebar entry in `web/src/app/layout/AppShell.tsx:38-53`, both hidden when the project declares no triage status. Create `web/src/features/inbox/` with `InboxPage.tsx` (two-pane layout), `InboxList.tsx` (infinite query with "Load more"), `InboxListItem.tsx` and `InboxDetail.tsx`, reusing `features/backlog/ItemBody.tsx` and the comments panel from `features/backlog/ItemDetail.tsx`. Filters (`pending`, `snoozed`, `all`) are URL state through a zod schema written next to `features/backlog/search.ts:58-73`.

## Acceptance Criteria

- [ ] The route renders a two-pane view, paginates incrementally and keeps its filter in the URL so a view is linkable.
- [ ] The sidebar shows a pending count and both the route and the entry disappear for a project with no triage status.
- [ ] Item bodies render through `web/src/markdown/sanitize.ts`.
- [ ] Vitest covers the filter schema and the empty state.
