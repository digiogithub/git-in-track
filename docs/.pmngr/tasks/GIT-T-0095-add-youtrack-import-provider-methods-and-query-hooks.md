---
id: GIT-T-0095
type: task
title: Add YouTrack import provider methods and query hooks
status: done
priority: medium
parent: GIT-US-0059
milestone: GIT-M-0011
author: mcp
labels: [web, agent-ok]
estimate: 3
created: 2026-09-13T13:17:38Z
updated: 2026-09-13T15:47:39Z
started: 2026-09-13T15:47:26Z
closed: 2026-09-13T15:47:39Z
---

## Description

Add `searchYoutrackIssues`, `previewYoutrackImport` and `runYoutrackImport` to the provider interface in `web/src/api/provider.ts` and implement them in every provider under `web/src/api/`, with the browser-only implementation throwing a clear "companion required" error rather than attempting a request. Add `web/src/features/youtrack/queries.ts` with the matching TanStack Query hooks, including a debounced search query keyed on the query text and preset.

## Acceptance Criteria

- [x] The three methods exist on the provider interface and in every implementation.
- [x] The browser-only provider throws a typed "companion required" error.
- [x] Hooks exist with stable query keys and the search hook is debounced.
- [x] Vitest covers the hooks and the browser-only failure with a mocked provider; `npm run test` passes.

## Notes

Landed as `searchYouTrackIssues`, `previewYouTrackImport` and `runYouTrackImport` (capital T, matching the five `getYouTrackSettings`-style methods already on the interface).

The companion implementations target `GET /api/v1/youtrack/issues` (story GIT-US-0054) and the two import routes over the vault's `youtrack.import.preview` / `youtrack.import.run`. **None of those three routes exists on the server yet**, so the companion path is written and typed but unexercised; the fake provider drives every test. `runYouTrackImport` answers `{jobId, result}` so that a companion which enqueues the import and one which runs it inline are both expressible.
