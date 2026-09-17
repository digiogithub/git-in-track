---
id: GIT-US-0103
type: story
title: Project search overlay on Ctrl+Shift+F
status: backlog
priority: medium
parent: GIT-EP-0021
milestone: GIT-M-0014
author: mcp
labels: [web]
estimate: 5
created: 2026-09-17T09:32:03Z
updated: 2026-09-17T09:32:03Z
---

## Description

As a user inside a project (backlog, inbox or KB), I want to press Ctrl+Shift+F to open a search overlay scoped to that project that finds items and KB pages, semantically and by text, the same way the workspace search does.

## Acceptance Criteria

- [ ] Ctrl+Shift+F (Cmd+Shift+F on macOS) opens a modal overlay on any `/p/:key/...` route; Escape closes it; focus returns to where it was.
- [ ] The overlay queries the provider `search` with the current project key and shows tabs All / Items / KB.
- [ ] Results reuse the workspace search hit rendering (snippet highlighting, semantic badge); arrow keys move the selection, Enter navigates to the item or page and closes the overlay.
- [ ] The shortcut does not fire while typing in the Markdown editor if it conflicts with an editor binding; it is listed wherever the app documents shortcuts.
- [ ] Component tests for open/close, tabs and navigation.
- [ ] docs/05 updated.
