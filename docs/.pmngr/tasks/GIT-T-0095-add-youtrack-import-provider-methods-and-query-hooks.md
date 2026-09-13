---
id: GIT-T-0095
type: task
title: Add YouTrack import provider methods and query hooks
status: todo
priority: medium
parent: GIT-US-0059
milestone: GIT-M-0011
author: mcp
labels: [web, agent-ok]
estimate: 3
created: 2026-09-13T13:17:38Z
updated: 2026-09-13T13:17:38Z
---

## Description

Add `searchYoutrackIssues`, `previewYoutrackImport` and `runYoutrackImport` to the provider interface in `web/src/api/provider.ts` and implement them in every provider under `web/src/api/`, with the browser-only implementation throwing a clear "companion required" error rather than attempting a request. Add `web/src/features/youtrack/queries.ts` with the matching TanStack Query hooks, including a debounced search query keyed on the query text and preset.

## Acceptance Criteria

- [ ] The three methods exist on the provider interface and in every implementation.
- [ ] The browser-only provider throws a typed "companion required" error.
- [ ] Hooks exist with stable query keys and the search hook is debounced.
- [ ] Vitest covers the hooks and the browser-only failure with a mocked provider; `npm run test` passes.
