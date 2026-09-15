---
id: GIT-T-0205
type: task
title: Build the PandoSearchCard settings card
status: in_review
priority: medium
parent: GIT-US-0091
milestone: GIT-M-0013
author: mcp
labels: [web]
estimate: 3
created: 2026-09-13T13:20:33Z
updated: 2026-09-15T16:21:36Z
started: 2026-09-15T16:21:14Z
---

## Description

Add `web/src/features/settings/PandoSearchCard.tsx` and compose it into `SettingsPage.tsx:40-136` next to `GitSettingsCard` and `McpToolsCard`, using the same plain `useState` plus `useEffect` load plus imperative save pattern — no react-hook-form and no zod in forms. It shows the Pando URL, the corpus directory, the index status and the selected backend, offers a reindex button with a progress state, and carries a standing warning that the embedding model is pinned configuration because Pando silently skips chunks whose vector length differs from the query's. Add the matching `DataProvider` methods and implement them in all four providers. The card is hidden in browser-only mode and when the feature is off.

## Acceptance Criteria

- [ ] The card renders the status, saves settings and reports whether the change was persisted.
- [ ] Reindex shows progress and surfaces a clear message when Pando is unreachable.
- [ ] The embedding-model warning is visible on the card.
- [ ] The card is hidden without the capability, and Vitest covers rendering, saving, reindex and the gate.
