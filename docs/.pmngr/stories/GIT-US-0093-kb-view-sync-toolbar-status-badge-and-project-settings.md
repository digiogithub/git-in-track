---
id: GIT-US-0093
type: story
title: KB view sync toolbar, status badge and project settings
status: backlog
priority: medium
parent: GIT-EP-0014
milestone: GIT-M-0011
author: mcp
labels: [web]
estimate: 5
created: 2026-09-13T13:15:19Z
updated: 2026-09-13T13:15:19Z
---

## Description

As a documentation owner, I want publish and sync controls in the KB view, so that I can push a page or a folder to YouTrack and see at a glance whether what is published is still current.

Add a toolbar group to `web/src/features/kb/KbViewer.tsx` with "Publish to YouTrack", "Sync now" and a status badge, rendered only when `capabilities.features.youtrack` is true and the project is linked. The badge shows the state returned by the KB status operation — unlinked, linked and in sync, out of date locally, out of date remotely, or conflict — with the article id and a link to the article when known, and it also appears per node in `KbTree.tsx` so a folder shows a summary of its children. Publishing a folder asks for confirmation and shows the number of pages it will touch.

State comes from a `useKbSyncStatus` hook in `web/src/features/kb/useKbData.ts` and is invalidated by the `sync.job.*` and `youtrack.kb.conflict` events; a conflict renders an inline notice linking to the generated `<page>.conflict.md` and explaining that the original page was not modified. The project setting `kb_sync: manual | on_write` and its direction `push | pull | both` are added to the YouTrack settings card, and when `on_write` is selected a short explanation makes clear that saving a page enqueues a publish. Provider methods `kbSyncStatus`, `publishKbPage` and `pullKbPage` are added to `web/src/api/provider.ts` and to every provider, with the browser-only one failing clearly.

## Acceptance Criteria

- [ ] Toolbar group and status badge appear only when the YouTrack feature is available and the project is linked.
- [ ] The badge renders the five states, shows the article id and links to the article when linked.
- [ ] Folder publish confirms and reports how many pages it will touch.
- [ ] A conflict renders an inline notice linking to `<page>.conflict.md` and stating the page was left untouched.
- [ ] Status invalidates on `sync.job.*` and `youtrack.kb.conflict` events without a manual refresh.
- [ ] `kb_sync` and its direction are editable in the YouTrack settings card with an explanation of `on_write`.
- [ ] Only design tokens are used; `npm run tokens:check` passes; Vitest covers gating, each badge state, folder confirmation and the conflict notice.

## Notes

Depends on the vault and REST operations of this epic and on GIT-EP-0011 for the capability flag and the settings card.

Existing code: `web/src/features/kb/KbViewer.tsx` (535 lines, tree/page/outline columns), `KbTree.tsx`, `useKbData.ts` (`useKbTree` :74, `useKbPage` :89, `useAddPageFeedback` :63-79), provider surface `web/src/api/provider.ts:975-988`. There is no KB edit UI — `provider.writePage` has no caller outside a test — so this story adds sync actions to a viewer, not an editor.

Do NOT build a KB editor here. Do NOT add a UI dependency; there is no `Popover` or `DropdownMenu` in the kit, so use a dialog or inline controls.
