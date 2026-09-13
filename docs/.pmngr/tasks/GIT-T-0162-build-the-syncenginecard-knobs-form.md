---
id: GIT-T-0162
type: task
title: Build the SyncEngineCard knobs form
status: todo
priority: medium
parent: GIT-US-0081
milestone: GIT-M-0011
author: mcp
labels: [web, agent-ok]
estimate: 3
created: 2026-09-13T13:19:15Z
updated: 2026-09-13T13:19:15Z
---

## Description

Create `web/src/features/settings/SyncEngineCard.tsx` and compose it into `SettingsPage.tsx:40-136` beside `SyncProxyCard`, with controlled inputs for workers, batch size, rate limit and max attempts plus the auto-push toggles, following the plain `useState` card pattern of `SyncProxyCard.tsx:44-72`. Validate ranges client side and save imperatively, showing a toast that states whether the change was persisted or process-only.

## Acceptance Criteria

- [ ] The card renders in companion mode only and loads the current settings on mount.
- [ ] Out-of-range values are blocked client side with an inline message before any request.
- [ ] Saving shows a toast distinguishing persisted from process-only.
- [ ] Vitest covers load, validate, save and the capability gating against the fake provider.
