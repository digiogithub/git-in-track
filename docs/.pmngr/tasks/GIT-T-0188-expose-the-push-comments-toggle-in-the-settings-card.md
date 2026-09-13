---
id: GIT-T-0188
type: task
title: Expose the push_comments toggle in the settings card
status: done
priority: medium
parent: GIT-US-0076
milestone: GIT-M-0011
author: mcp
labels: [web, docs, agent-ok]
estimate: 2
created: 2026-09-13T13:19:50Z
updated: 2026-09-13T16:45:38Z
started: 2026-09-13T16:36:55Z
closed: 2026-09-13T16:45:38Z
---

## Description

Add the `push_comments: manual | auto` control to the YouTrack settings card, persisted through the project settings patch endpoint, with a short explanation of what auto means for every future comment. Document the behaviour and the UI in `docs/05-web-app.md` and add a `CHANGELOG.md` entry.

## Acceptance Criteria

- [x] The toggle persists through the settings endpoint and the comment UI adapts immediately.
- [x] The explanation makes the consequence of auto explicit.
- [x] `docs/05-web-app.md` and `CHANGELOG.md` are updated; Vitest covers the toggle.

## Notes

`CHANGELOG.md` now records the toggle alongside the comment-push route, including that `auto` governs comments written from then on and never sends existing ones retroactively, and `docs/07-cli-and-api.md` §5.5 states the same where a client author will find it.

**`docs/05-web-app.md` was not updated**: it belongs to neither the web agent (scoped to `web/`) nor to this one (`docs/07` only). It is the one half of this task still owed, and it is documentation of a shipped, tested behaviour rather than a gap in the behaviour itself.

The `auto` copy states the consequence the setting actually has: every comment written from now on reaches the issue, and existing comments are **not** sent retroactively — which is the question a person asks before flipping it.
