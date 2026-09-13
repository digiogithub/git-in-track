---
id: GIT-T-0057
type: task
title: Add the YouTrack methods to all four DataProviders
status: todo
priority: medium
parent: GIT-US-0055
milestone: GIT-M-0011
author: mcp
labels: [web, agent-ok]
estimate: 3
created: 2026-09-13T13:16:44Z
updated: 2026-09-13T13:16:44Z
---

## Description

Add `getYouTrackSettings`, `updateYouTrackSettings`, `testYouTrackConnection`, `listYouTrackProjects` and `listYouTrackFields` to the `DataProvider` interface (`web/src/api/provider.ts`) and implement them in the companion provider against the new REST endpoints, and in the browser and fake providers as a clear "not available in this mode" failure. Map `features.youtrack` into `Capabilities` in `toCapabilities` (`web/src/api/companion-provider.ts:751-766`) and into the browser defaults as always false.

## Acceptance Criteria

- [ ] All five methods exist on the interface and in the companion, browser and fake providers.
- [ ] `Capabilities` carries `youtrack` and the browser provider reports it false.
- [ ] `tsc` and ESLint pass; Vitest covers the companion provider's request shapes against a mocked fetch.
