---
id: GIT-T-0217
type: task
title: Add the KB toolbar actions and status badge
status: todo
priority: medium
parent: GIT-US-0093
milestone: GIT-M-0011
author: mcp
labels: [web, agent-ok]
estimate: 3
created: 2026-09-13T13:21:30Z
updated: 2026-09-13T13:21:30Z
---

## Description

Add the toolbar group — "Publish to YouTrack", "Sync now" and a status badge — to `web/src/features/kb/KbViewer.tsx`, rendered only when `capabilities.features.youtrack` is true and the project is linked, plus a compact badge per node in `KbTree.tsx` summarising a folder's children. The badge shows the article id and links to the article when linked. Folder publish asks for confirmation and states how many pages it will touch.

## Acceptance Criteria

- [ ] The toolbar and badges render under the documented gating and are absent in browser-only mode.
- [ ] The badge renders the five states and links to the article when linked.
- [ ] Folder publish confirms and reports the page count.
- [ ] Vitest covers gating, each badge state and the confirmation; `npm run tokens:check` passes.
