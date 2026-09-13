---
id: GIT-T-0218
type: task
title: Render the conflict notice in the KB viewer
status: done
priority: medium
parent: GIT-US-0093
milestone: GIT-M-0011
author: mcp
labels: [web, agent-ok]
estimate: 2
created: 2026-09-13T13:21:34Z
updated: 2026-09-13T16:34:38Z
started: 2026-09-13T16:34:27Z
closed: 2026-09-13T16:34:38Z
---

## Description

When a page's sync state is `conflict`, render an inline notice above the page in `KbViewer.tsx` linking to the generated `<page>.conflict.md` and stating plainly that the original page was not modified and which side is newer. The notice must also appear when the conflict arrives while the page is open, via the `youtrack.kb.conflict` event.

## Acceptance Criteria

- [x] A conflicting page shows an inline notice linking to the conflict page.
- [x] The notice states that the original page was untouched and which side is newer.
- [x] The notice appears live when the conflict event arrives for the open page.
- [x] Vitest covers both the initial-load and the live-event path.

## Notes

Conflict frames are kept as events rather than derived from the status query,
because only the frame carries `conflictPath`. A notice rendered from a plain
status load falls back to `conflictPathOf(path)`, which mirrors the companion's
own `TrimSuffix(path, ".md") + ".conflict.md"`.
