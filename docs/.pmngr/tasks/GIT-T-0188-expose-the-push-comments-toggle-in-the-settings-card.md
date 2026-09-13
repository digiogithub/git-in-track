---
id: GIT-T-0188
type: task
title: Expose the push_comments toggle in the settings card
status: in_review
priority: medium
parent: GIT-US-0076
milestone: GIT-M-0011
author: mcp
labels: [web, docs, agent-ok]
estimate: 2
created: 2026-09-13T13:19:50Z
updated: 2026-09-13T16:37:07Z
started: 2026-09-13T16:36:55Z
---

## Description

Add the `push_comments: manual | auto` control to the YouTrack settings card, persisted through the project settings patch endpoint, with a short explanation of what auto means for every future comment. Document the behaviour and the UI in `docs/05-web-app.md` and add a `CHANGELOG.md` entry.

## Acceptance Criteria

- [x] The toggle persists through the settings endpoint and the comment UI adapts immediately.
- [x] The explanation makes the consequence of auto explicit.
- [ ] `docs/05-web-app.md` and `CHANGELOG.md` are updated; Vitest covers the toggle.

## Notes

The third criterion is split: **Vitest covers the toggle**, but the two
documents were **not** updated. This wave's web agent was scoped to `web/`
only, with `docs/` owned by other agents editing the same working tree, so
touching `docs/05-web-app.md` or `CHANGELOG.md` would have raced them. The
documentation half is left for whoever owns those files.

The `auto` copy states the consequence the setting actually has: every comment
written from now on reaches the issue, and existing comments are **not** sent
retroactively — which is the question a person asks before flipping it.
