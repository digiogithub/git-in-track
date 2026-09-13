---
id: GIT-T-0218
type: task
title: Render the conflict notice in the KB viewer
status: todo
priority: medium
parent: GIT-US-0093
milestone: GIT-M-0011
author: mcp
labels: [web, agent-ok]
estimate: 2
created: 2026-09-13T13:21:34Z
updated: 2026-09-13T13:21:34Z
---

## Description

When a page's sync state is `conflict`, render an inline notice above the page in `KbViewer.tsx` linking to the generated `<page>.conflict.md` and stating plainly that the original page was not modified and which side is newer. The notice must also appear when the conflict arrives while the page is open, via the `youtrack.kb.conflict` event.

## Acceptance Criteria

- [ ] A conflicting page shows an inline notice linking to the conflict page.
- [ ] The notice states that the original page was untouched and which side is newer.
- [ ] The notice appears live when the conflict event arrives for the open page.
- [ ] Vitest covers both the initial-load and the live-event path.
