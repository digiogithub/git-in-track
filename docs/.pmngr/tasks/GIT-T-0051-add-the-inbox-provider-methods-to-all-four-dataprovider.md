---
id: GIT-T-0051
type: task
title: Add the inbox provider methods to all four DataProvider implementations
status: todo
priority: medium
parent: GIT-US-0060
milestone: GIT-M-0012
author: mcp
labels: [web, agent-ok]
estimate: 3
created: 2026-09-13T13:16:35Z
updated: 2026-09-13T13:16:35Z
---

## Description

Declare `listInbox(filter)`, `createInboxItem(draft)` and `triageInboxItem(input)` on the `DataProvider` interface in `web/src/api/provider.ts` with their request and response types, then implement them in the companion provider (REST against the new endpoints), the browser provider (`vault.Call` over WASM), and the fake provider used by tests. Add TanStack Query hooks in a new `web/src/features/inbox/queries.ts` modelled on `web/src/features/boards/sprint-queries.ts`, including the query-key layout and the invalidation the `inbox.changed` WS event triggers.

## Acceptance Criteria

- [ ] All four implementations compile and the interface has no method a provider leaves unimplemented.
- [ ] Query keys are namespaced per project and invalidated by `inbox.changed`.
- [ ] A Vitest suite against the fake provider covers list, create and each triage action.
- [ ] `npm run lint` and `tsc` pass.
