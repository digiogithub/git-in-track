---
id: GIT-T-0138
type: task
title: Document the corpus layout and the Pando KBPath warning
status: in_review
priority: medium
parent: GIT-US-0073
milestone: GIT-M-0013
author: mcp
labels: [docs, agent-ok]
estimate: 1
created: 2026-09-13T13:18:41Z
updated: 2026-09-15T15:25:31Z
started: 2026-09-15T15:16:25Z
---

## Description

Document the corpus in `docs/07-cli-and-api.md` and in the agent interface document: where it lives, its directory layout, the front matter each document carries, that it is derived data outside the repository and safe to delete, and that `[Remembrances] KBPath` must point at it and never at the repository root — Pando's directory walk and watcher have no `node_modules` or dot-directory exclusions and will exhaust inotify watches. Add a `CHANGELOG.md` entry.

## Acceptance Criteria

- [ ] The layout, front matter and derived-data status are documented.
- [ ] The KBPath warning is stated where a reader configuring Pando will see it.
- [ ] `CHANGELOG.md` records the exporter.
