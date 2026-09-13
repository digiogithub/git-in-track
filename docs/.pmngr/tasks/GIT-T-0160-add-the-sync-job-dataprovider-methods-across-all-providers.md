---
id: GIT-T-0160
type: task
title: Add the sync job DataProvider methods across all providers
status: todo
priority: medium
parent: GIT-US-0081
milestone: GIT-M-0011
author: mcp
labels: [web, agent-ok]
estimate: 2
created: 2026-09-13T13:19:10Z
updated: 2026-09-13T13:19:10Z
---

## Description

Add `listSyncJobs`, `getSyncJob`, `retrySyncJob` and `cancelSyncJob` to the `DataProvider` interface (`web/src/api/provider.ts`) and implement them in the companion provider against the new endpoints, and in the browser and fake providers — the fake returning scriptable fixtures so the card's tests need no network. Extend the existing sync settings methods with the engine knobs.

## Acceptance Criteria

- [ ] The four methods plus the extended settings pair exist on the interface and in all providers.
- [ ] The fake provider can script jobs in every state for the card tests.
- [ ] `tsc` and ESLint pass; Vitest covers the companion provider's request shapes.
