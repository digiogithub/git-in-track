---
id: GIT-T-0188
type: task
title: Expose the push_comments toggle in the settings card
status: todo
priority: medium
parent: GIT-US-0076
milestone: GIT-M-0011
author: mcp
labels: [web, docs, agent-ok]
estimate: 2
created: 2026-09-13T13:19:50Z
updated: 2026-09-13T13:19:50Z
---

## Description

Add the `push_comments: manual | auto` control to the YouTrack settings card, persisted through the project settings patch endpoint, with a short explanation of what auto means for every future comment. Document the behaviour and the UI in `docs/05-web-app.md` and add a `CHANGELOG.md` entry.

## Acceptance Criteria

- [ ] The toggle persists through the settings endpoint and the comment UI adapts immediately.
- [ ] The explanation makes the consequence of auto explicit.
- [ ] `docs/05-web-app.md` and `CHANGELOG.md` are updated; Vitest covers the toggle.
