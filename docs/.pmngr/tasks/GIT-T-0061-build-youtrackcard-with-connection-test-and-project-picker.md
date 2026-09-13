---
id: GIT-T-0061
type: task
title: Build YouTrackCard with connection test and project picker
status: done
priority: medium
parent: GIT-US-0055
milestone: GIT-M-0011
author: mcp
labels: [web, agent-ok]
estimate: 5
created: 2026-09-13T13:16:49Z
updated: 2026-09-13T15:04:43Z
started: 2026-09-13T14:58:45Z
closed: 2026-09-13T15:04:43Z
---

## Description

Create `web/src/features/settings/YouTrackCard.tsx` and compose it into `SettingsPage.tsx:40-136` behind the `youtrack` capability. Follow the existing card pattern — plain `useState` with controlled inputs, a `useEffect` load and an imperative save, as in `SyncProxyCard.tsx:44-72`. Include the instance URL, a password-type token field that shows only whether a token is stored and where it came from, a "Test connection" button reporting the resolved user or a precise error, the project `Combobox` backed by TanStack Query, and toggles for comment push and KB sync.

## Acceptance Criteria

- [x] The card renders only when the capability is true and is absent in browser-only mode.
- [x] Test connection shows the YouTrack user on success and a distinct message for bad token, missing permission and wrong base URL.
- [x] Saving shows a toast stating whether the change was persisted to disk or applied only to the running process.
- [x] Vitest covers load, test, save and capability gating using the fake provider.

## Notes

The card is gated on `capabilities.youtrackSupported` (companion mode), not on `capabilities.youtrack` (a project already linked): a card that appeared only once a project was connected could never connect the first one. Comment push and KB sync are native `<select>`s rather than switches, because both are enums (`manual|auto`, `manual|on_write`, `push|pull|both`) and not booleans.
