---
id: GIT-T-0190
type: task
title: Add the semantic results toggle to the UI preferences
status: done
priority: medium
parent: GIT-US-0086
milestone: GIT-M-0013
author: mcp
labels: [web, agent-ok]
estimate: 1
created: 2026-09-13T13:19:54Z
updated: 2026-09-15T16:44:41Z
started: 2026-09-15T15:50:38Z
closed: 2026-09-15T16:44:41Z
---

## Description

Add a toggle that hides the semantic section, storing the preference in `web/src/app/ui-prefs.ts` alongside the other per-user layout settings so it persists in `localStorage` under the existing `gintrack:ui-prefs` key. The toggle is rendered only when the capability makes the section available.

## Acceptance Criteria

- [ ] The toggle hides and restores the section and persists across a reload.
- [ ] It is absent when the capability is not `'pando'`.
- [ ] Vitest covers persistence and the capability gate.
