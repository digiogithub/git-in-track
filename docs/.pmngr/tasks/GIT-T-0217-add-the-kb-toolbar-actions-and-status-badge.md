---
id: GIT-T-0217
type: task
title: Add the KB toolbar actions and status badge
status: done
priority: medium
parent: GIT-US-0093
milestone: GIT-M-0011
author: mcp
labels: [web, agent-ok]
estimate: 3
created: 2026-09-13T13:21:30Z
updated: 2026-09-13T16:34:24Z
started: 2026-09-13T16:34:12Z
closed: 2026-09-13T16:34:24Z
---

## Description

Add the toolbar group — "Publish to YouTrack", "Sync now" and a status badge — to `web/src/features/kb/KbViewer.tsx`, rendered only when `capabilities.features.youtrack` is true and the project is linked, plus a compact badge per node in `KbTree.tsx` summarising a folder's children. The badge shows the article id and links to the article when linked. Folder publish asks for confirmation and states how many pages it will touch.

## Acceptance Criteria

- [x] The toolbar and badges render under the documented gating and are absent in browser-only mode.
- [x] The badge renders the five states and links to the article when linked.
- [x] Folder publish confirms and reports the page count.
- [x] Vitest covers gating, each badge state and the confirmation; `npm run tokens:check` passes.

## Notes

"Sync now" is bound to **pull** and the remote re-read is a separate "Check the
article" button, so neither action is ambiguous about direction.

The folder page count comes from the KB tree rather than from a status round
trip, so the confirmation can state the count before anything is queued.

The tree-wide status query never asks for the remote side, so badging a whole
handbook costs no request against someone else's rate limit.
