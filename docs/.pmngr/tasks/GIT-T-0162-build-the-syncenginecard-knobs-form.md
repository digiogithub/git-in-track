---
id: GIT-T-0162
type: task
title: Build the SyncEngineCard knobs form
status: done
priority: medium
parent: GIT-US-0081
milestone: GIT-M-0011
author: mcp
labels: [web, agent-ok]
estimate: 3
created: 2026-09-13T13:19:15Z
updated: 2026-09-13T15:49:06Z
started: 2026-09-13T15:48:52Z
closed: 2026-09-13T15:49:06Z
---

## Description

Create `web/src/features/settings/SyncEngineCard.tsx` and compose it into `SettingsPage.tsx:40-136` beside `SyncProxyCard`, with controlled inputs for workers, batch size, rate limit and max attempts plus the auto-push toggles, following the plain `useState` card pattern of `SyncProxyCard.tsx:44-72`. Validate ranges client side and save imperatively, showing a toast that states whether the change was persisted or process-only.

## Acceptance Criteria

- [x] The card renders in companion mode only and loads the current settings on mount.
- [x] Out-of-range values are blocked client side with an inline message before any request.
- [x] Saving shows a toast distinguishing persisted from process-only.
- [x] Vitest covers load, validate, save and the capability gating against the fake provider.

## Notes

Gated on the reported surface rather than on the provider kind, as the story asks: a runtime whose sync settings carry no `engine` half has no engine and the card is absent — same shape as `SyncProxyCard` hiding itself when `proxySource` is absent. There is no dedicated engine capability on `GET /capabilities` to gate on.

The ranges are `SYNC_ENGINE_RANGES` in `provider.ts`, transcribed from docs/07 §4.1 (workers 1–64, batch 1–500, rate ≤ 1000, attempts 1–20), and the inline message names the field and the bounds. `persisted` is `false` for any engine change today, and the toast says so in those words rather than claiming success.

`SettingsPage` has no `ToastProvider` of its own; the host now lives in `AppShell`, which is also what makes the background toasts of GIT-T-0169 reachable from every route.
