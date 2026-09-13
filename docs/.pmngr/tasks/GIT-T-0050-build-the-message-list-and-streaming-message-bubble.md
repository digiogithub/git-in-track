---
id: GIT-T-0050
type: task
title: Build the message list and streaming message bubble
status: todo
priority: medium
parent: GIT-US-0057
milestone: GIT-M-0013
author: mcp
labels: [web]
estimate: 5
created: 2026-09-13T13:16:33Z
updated: 2026-09-13T13:16:33Z
---

## Description

Add `MessageList.tsx` and `MessageBubble.tsx` under `web/src/features/agent/`. Assistant text renders through `web/src/markdown/` via its public `index.ts` surface only, so sanitisation still runs last; user messages render as escaped plain text. Streaming appends deltas without re-parsing the whole document on every frame — parse on a throttle and render the tail as plain text in between. Auto-scroll only while the user is already at the bottom, and show a "jump to latest" affordance otherwise. Mark the list as a polite live region.

## Acceptance Criteria

- [ ] A streaming assistant message renders progressively and ends as fully parsed Markdown.
- [ ] Raw HTML inside agent output is not executed.
- [ ] Auto-scroll follows only when pinned to the bottom; scrolling up stops it.
- [ ] Vitest covers streaming render, the scroll behaviour and sanitisation.
