---
id: GIT-T-0062
type: task
title: Persist the thread list and add the page shell
status: in_review
priority: medium
parent: GIT-US-0057
milestone: GIT-M-0013
author: mcp
labels: [web]
estimate: 3
created: 2026-09-13T13:16:49Z
updated: 2026-09-15T16:00:47Z
started: 2026-09-15T15:45:50Z
---

## Description

Add `AgentPage.tsx`, the three-column shell composing the thread list, the message list with composer, and a slot for the state panel. Persist thread metadata `{id, title, updatedAt}` to `localStorage` under `gintrack:agent:threads`, following the pattern of `web/src/app/ui-prefs.ts`; the title is derived from the first user message. Transcripts are not persisted — Pando owns the history keyed by thread id, and the store rehydrates a thread by reconnecting, so the local list is derived data only.

## Acceptance Criteria

- [ ] The shell lays out at desktop and narrow widths without horizontal overflow.
- [ ] The thread list survives a reload and new threads appear at the top with a derived title.
- [ ] Deleting a thread removes it from storage and selects a neighbour.
- [ ] Vitest covers persistence, title derivation and deletion.
