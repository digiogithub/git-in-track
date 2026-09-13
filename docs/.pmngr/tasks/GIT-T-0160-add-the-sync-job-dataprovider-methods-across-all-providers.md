---
id: GIT-T-0160
type: task
title: Add the sync job DataProvider methods across all providers
status: done
priority: medium
parent: GIT-US-0081
milestone: GIT-M-0011
author: mcp
labels: [web, agent-ok]
estimate: 2
created: 2026-09-13T13:19:10Z
updated: 2026-09-13T15:48:49Z
started: 2026-09-13T15:48:36Z
closed: 2026-09-13T15:48:49Z
---

## Description

Add `listSyncJobs`, `getSyncJob`, `retrySyncJob` and `cancelSyncJob` to the `DataProvider` interface (`web/src/api/provider.ts`) and implement them in the companion provider against the new endpoints, and in the browser and fake providers — the fake returning scriptable fixtures so the card's tests need no network. Extend the existing sync settings methods with the engine knobs.

## Acceptance Criteria

- [x] The four methods plus the extended settings pair exist on the interface and in all providers.
- [x] The fake provider can script jobs in every state for the card tests.
- [x] `tsc` and ESLint pass; Vitest covers the companion provider's request shapes.

## Notes

`getSyncSettings` now reads `GET /api/v1/sync/settings` (both halves in one document) instead of digging the settings out of `GET /sync/status`, and `updateSyncSettings` sends the engine knobs nested under `engine`, which is the form that wins when a companion is sent both.

The three problem codes are mapped explicitly (`sync_job_not_found`, `sync_job_not_retryable`, `sync_engine_not_running`): without that, the 409 of a refused transition would have been read as `stale_revision`, which is the default for 409 everywhere else in this API.

`SyncSettings.engine` is optional and its absence is what "this runtime has no engine" means — the browser provider reports none and fails the four calls loudly. `FakeData.syncEngine` scripts jobs in every state plus a refusing retry or cancel.
