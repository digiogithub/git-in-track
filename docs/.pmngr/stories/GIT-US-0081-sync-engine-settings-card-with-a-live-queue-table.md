---
id: GIT-US-0081
type: story
title: Sync engine settings card with a live queue table
status: done
priority: medium
parent: GIT-EP-0015
milestone: GIT-M-0011
author: mcp
labels: [web]
estimate: 8
created: 2026-09-13T13:14:16Z
updated: 2026-09-13T15:50:45Z
started: 2026-09-13T15:50:26Z
closed: 2026-09-13T15:50:45Z
---

## Description

As a user, I want a Settings card showing the sync queue live with the engine's knobs above it, so that I can see what is queued, running or stuck, retry a failure and tune throughput without reading a log file.

Add `SyncEngineCard` to `web/src/features/settings/` and compose it into `SettingsPage.tsx:40-136` beside `SyncProxyCard`. The top half is the knobs — workers, batch size, rate limit, max attempts and the auto-push toggles — following the card pattern already in use: plain `useState` with controlled inputs, a `useEffect` load and an imperative save, with hand-rolled validation in the spirit of `features/editor/front-matter.ts:273`. The bottom half is a table of jobs (kind, key, state, attempts, next attempt, last error) fed by TanStack Query and kept fresh by the `sync.job.*` subscription from the events story, with per-row Retry and Cancel actions. Job counts by state sit in the card header so the state is legible at a glance.

New `DataProvider` methods (`listSyncJobs`, `retrySyncJob`, `cancelSyncJob`, and the settings pair if not already present) must be added to the interface and implemented in all four providers; feature code never calls `fetch('/api/…')` directly. The card renders only in companion mode, gated on the capability rather than on the provider kind, and error text coming from a third-party system is rendered as plain text, never as unsanitised Markdown.

## Acceptance Criteria

- [x] `SyncEngineCard` renders in `SettingsPage` in companion mode only and is absent in browser-only mode.
- [x] Workers, batch size, rate limit and max attempts are editable, client-validated for range, and saved with a toast stating whether the change was persisted or process-only.
- [x] The queue table lists jobs with kind, key, state, attempts, next attempt and last error, and updates live from `sync.job.*` without polling.
- [x] Retry and Cancel actions work per row, are disabled for states where they do not apply, and show a toast on success and on failure.
- [x] A job failing in the background raises a toast even when the user is not on the Settings page.
- [x] Error text from YouTrack is rendered as plain text, and any Markdown preview path goes through `web/src/markdown/sanitize.ts`.
- [x] The new `DataProvider` methods exist on the interface and in the companion, browser and fake providers.
- [x] Vitest covers the knobs form, the table rendering from a fake provider, the retry and cancel actions and the capability gating.

## Notes

Card precedents: `SyncProxyCard.tsx:44-72` and `GitSettingsCard.tsx:47,62-66` for the form; `TeamProjectsCard.tsx:101-110` for the TanStack Query parts; `features/backlog/ItemTable.tsx` for a table with URL-held state if sorting is wanted.

Imported YouTrack error strings and job payloads are untrusted third-party content and must be treated as data, exactly as repository content is (`docs/10-development-guidelines.md:717-724`).

Do NOT poll the jobs endpoint on a timer: the WebSocket stream already exists and polling would fight with it. Do NOT add a new table library — TanStack Table is already a dependency and used by the backlog view.

**Landed.** Two points a reviewer should know. The gate is the presence of `SyncSettings.engine` rather than a capability flag: `GET /capabilities` declares no engine feature, and the sync settings document already answers the same question truthfully. And the queue table is a plain `<Table>`, not TanStack Table: there is no sorting, filtering or selection on it, so the headless table would have been ceremony around four `map`s.
