---
id: GIT-T-0219
type: task
title: Add the kb_sync setting and document the KB sync UI
status: todo
priority: medium
parent: GIT-US-0093
milestone: GIT-M-0011
author: mcp
labels: [web, docs, agent-ok]
estimate: 2
created: 2026-09-13T13:21:42Z
updated: 2026-09-13T13:21:42Z
---

## Description

Add `kb_sync: manual | on_write` and the direction `push | pull | both` to the YouTrack settings card, persisted through the project settings patch endpoint, with a short explanation that `on_write` enqueues a publish whenever a page is saved. Document the KB sync surface in `docs/05-web-app.md` and add a `CHANGELOG.md` entry.

## Acceptance Criteria

- [ ] Both settings persist through the settings endpoint and the KB toolbar reflects them.
- [ ] The explanation of `on_write` is explicit about the consequence.
- [ ] `docs/05-web-app.md` and `CHANGELOG.md` are updated; Vitest covers the controls.
