---
id: GIT-US-0059
type: story
title: Backlog Import from YouTrack dialog
status: done
priority: high
parent: GIT-EP-0012
milestone: GIT-M-0011
author: mcp
labels: [web]
estimate: 13
created: 2026-09-13T13:12:28Z
updated: 2026-09-13T16:21:21Z
started: 2026-09-13T15:51:00Z
closed: 2026-09-13T16:21:21Z
---

## Description

As a product owner, I want an "Import from YouTrack" dialog in the Backlog, so that I can search the linked YouTrack project, pick issues, see exactly what will be created or updated and run the import with visible progress.

Add `web/src/features/youtrack/ImportDialog.tsx` plus `ImportQueryBar.tsx`, `ImportPreviewTable.tsx`, `ImportOptions.tsx` and `queries.ts`, opened from a toolbar entry in the backlog view (`web/src/features/backlog/ItemTable.tsx` header area) that is rendered only when `capabilities.features.youtrack` is true and the project is linked. The query bar is a debounced combobox over `GET /api/v1/youtrack/issues` built on the same hand-rolled ARIA pattern as `web/src/components/editor/ItemPicker.tsx:29-163` (200 ms debounce, `role="combobox"`, `role="listbox"`, blur grace), with preset chips for epics, stories, tasks, versions and unresolved, and multi-select with a running count. Results already imported show their gintrack id and are selectable for an update.

Options are include subtasks with a depth stepper, include linked issues, include comments, include attachments, and a disabled "land in Inbox" checkbox with a tooltip pointing at the future Inbox epic. Preview calls the preview operation and renders a table of create-vs-update rows with mapped type, status, parent and any warnings; Run enqueues the job, closes into a progress strip driven by the `sync.job.*` WebSocket events, and ends in a result summary listing created, updated and failed issues with a link to each new item. Provider methods `searchYoutrackIssues`, `previewYoutrackImport` and `runYoutrackImport` are added to `web/src/api/provider.ts` and to every provider implementation, with the browser-only provider throwing a clear "companion required" error.

## Acceptance Criteria

- [x] Toolbar entry appears only when `features.youtrack` is reported and the project is linked; it is absent in browser-only mode.
- [x] Debounced autosuggest with preset chips, multi-select, and an "already imported" marker carrying the gintrack id.
- [x] Options for subtasks depth, linked issues, comments and attachments are sent to preview and run; the Inbox option is visibly disabled.
- [x] Preview table shows create vs update, mapped type, status, parent and warnings before anything is written.
- [x] Run shows live progress from `sync.job.progress` and a final summary with created, updated and failed counts and per-issue errors.
- [x] Provider methods are added to all provider implementations; the browser-only one fails with a clear message rather than a network error.
- [x] The dialog is keyboard navigable and uses design tokens only; `npm run tokens:check` passes.
- [x] Vitest covers the combobox behaviour, preset switching, preview rendering and the progress-to-summary transition with a mocked provider.

## Notes

Depends on the search endpoint and the vault operations of this epic, on GIT-EP-0011 for `features.youtrack`, and on GIT-EP-0015 for the `sync.job.*` events.

Reuse rather than add: there is no `cmdk`, no shadcn `Command`, `Popover` or `DropdownMenu` in `web/src/components/ui/` — `ItemPicker.tsx` is the typeahead to copy, and GIT-EP-0011 extracts a generic `Combobox` from it. Event bridging follows `web/src/features/backlog/queries.ts:130-148`. Filter state in the backlog lives in the URL (`web/src/features/backlog/search.ts`); the dialog's own state does not belong there.

Do NOT introduce a new UI dependency for the dialog. Do NOT write items from the frontend — the dialog only calls the import operations.

**Held at `in_review`, not `done`, and deliberately.** Every criterion above is met against the provider seam and the whole flow is covered by Vitest with the fake provider, but the feature cannot work against a real companion yet: **none of the three HTTP routes it calls exists**. `GET /api/v1/youtrack/issues` is GIT-US-0054 (`backlog`), and the import preview/run routes over the vault's `youtrack.import.preview` / `youtrack.import.run` (GIT-US-0047, `in_review`) have no REST surface and no story. GIT-T-0111 (documentation) is also still `todo`. This story becomes `done` when those land and the dialog is exercised end to end once.
