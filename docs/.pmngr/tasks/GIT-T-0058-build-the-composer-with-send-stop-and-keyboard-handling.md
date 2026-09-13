---
id: GIT-T-0058
type: task
title: Build the composer with send, stop and keyboard handling
status: todo
priority: medium
parent: GIT-US-0057
milestone: GIT-M-0013
author: mcp
labels: [web, agent-ok]
estimate: 3
created: 2026-09-13T13:16:44Z
updated: 2026-09-13T13:16:44Z
---

## Description

Add `Composer.tsx` over `web/src/components/ui/textarea.tsx` and `button.tsx`: an auto-growing textarea, Enter to send and Shift+Enter for a newline, a send button disabled while empty, and a stop button that replaces it during a run and calls the store's cancel action. Disable sending while a run is interrupted awaiting a dialog answer, with a short explanation of why.

## Acceptance Criteria

- [ ] Enter sends, Shift+Enter inserts a newline, and the textarea grows to a bounded maximum height.
- [ ] Stop cancels the run and the composer returns to the sendable state with the draft preserved.
- [ ] Sending is blocked while an interrupt is pending and the reason is shown.
- [ ] Vitest covers the keyboard behaviour, stop and the blocked state.
