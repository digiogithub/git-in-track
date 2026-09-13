---
id: GIT-T-0051
type: task
title: Add the inbox provider methods to all four DataProvider implementations
status: done
priority: medium
parent: GIT-US-0060
milestone: GIT-M-0012
author: mcp
labels: [web, agent-ok]
estimate: 3
created: 2026-09-13T13:16:35Z
updated: 2026-09-13T16:39:39Z
started: 2026-09-13T16:39:26Z
closed: 2026-09-13T16:39:39Z
---

## Description

Declare `listInbox(filter)`, `createInboxItem(draft)` and `triageInboxItem(input)` on the `DataProvider` interface in `web/src/api/provider.ts` with their request and response types, then implement them in the companion provider (REST against the new endpoints), the browser provider (`vault.Call` over WASM), and the fake provider used by tests. Add TanStack Query hooks in a new `web/src/features/inbox/queries.ts` modelled on `web/src/features/boards/sprint-queries.ts`, including the query-key layout and the invalidation the `inbox.changed` WS event triggers.

## Acceptance Criteria

- [x] All four implementations compile and the interface has no method a provider leaves unimplemented.
- [x] Query keys are namespaced per project and invalidated by `inbox.changed`.
- [x] A Vitest suite against the fake provider covers list, create and each triage action.
- [x] `npm run lint` and `tsc` pass.

## Notes

`InboxTriageInput` deliberately carries no `type` field. An item id encodes its
type for life (R-ID-3), so "this should have been an epic" is answered by
creating the right item and marking this one a duplicate — the surface must not
be able to offer a type picker on accept at all.

The browser provider raises the `inbox` change event itself after a local
triage, since there is no WebSocket in that mode. An open pane and the sidebar
badge therefore subscribe to one event in both runtimes and need no second code
path — which is what task GIT-T-0064's "browser-only fallback" amounts to.
