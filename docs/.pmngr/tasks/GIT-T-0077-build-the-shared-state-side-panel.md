---
id: GIT-T-0077
type: task
title: Build the shared-state side panel
status: todo
priority: medium
parent: GIT-US-0061
milestone: GIT-M-0013
author: mcp
labels: [web]
estimate: 3
created: 2026-09-13T13:17:12Z
updated: 2026-09-13T13:17:12Z
---

## Description

Add `StatePanel.tsx` rendering the shared state document from the store: the todo list with statuses, sub-agents with their task state, token usage with a context-window progress bar over `web/src/components/ui/progress.tsx`, and the touched-files list. The document belongs to the thread rather than the run, so render from the last snapshot with deltas applied and never clear it on `RUN_STARTED`. Collapse the panel on narrow viewports.

## Acceptance Criteria

- [ ] All four sections render and update live from state deltas.
- [ ] The panel keeps its contents between turns of the same thread.
- [ ] Empty sections are omitted rather than shown as empty headings.
- [ ] Vitest covers rendering from a snapshot, a delta update and persistence across a new run.
