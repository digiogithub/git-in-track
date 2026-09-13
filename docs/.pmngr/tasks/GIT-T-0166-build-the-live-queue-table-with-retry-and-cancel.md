---
id: GIT-T-0166
type: task
title: Build the live queue table with retry and cancel
status: todo
priority: medium
parent: GIT-US-0081
milestone: GIT-M-0011
author: mcp
labels: [web, agent-ok]
estimate: 5
created: 2026-09-13T13:19:20Z
updated: 2026-09-13T13:19:20Z
---

## Description

Add the queue table to the card: kind, key, state, attempts, next attempt and last error per row, with counts by state in the header, fed by TanStack Query and kept fresh by `useSyncJobEvents` rather than polling. Add per-row Retry and Cancel buttons, disabled in states where the action does not apply. Error text arriving from YouTrack is untrusted and must be rendered as plain text, never as raw HTML or unsanitised Markdown.

## Acceptance Criteria

- [ ] The table renders every job state and updates live from `sync.job.*` with no timer-based polling.
- [ ] Retry and Cancel act per row, are disabled where inapplicable and show success and failure toasts.
- [ ] Error text is rendered as plain text and cannot inject markup.
- [ ] Vitest covers rendering from the fake provider, the live update path and both actions.
