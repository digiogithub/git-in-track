---
id: GIT-US-0183
type: story
title: Fix stale text about Pando in spec impact help and ADR-039
status: backlog
priority: low
author: mcp
labels: [cli, docs, agent-ok, good-first-issue]
created: 2026-09-29T22:56:37Z
updated: 2026-09-29T22:56:37Z
---

## Description

`gintrack spec impact --help` still says the CLI has no Pando client, but since GIT-US-0176 it discovers a managed instance. ADR-039's status line says "nothing implemented" although GIT-US-0173..0177 are merged, and its context still quotes the old CORS `*` claim fixed by GIT-US-0172 (#107 §10.6).

## Acceptance Criteria

- [ ] `spec impact --help` describes how tiers 2–3 find Pando (external config or a running managed instance).
- [ ] ADR-039 gets a dated implementation note (status stays `proposed` until the maintainer accepts it) and a note that the CORS claim is historical.
- [ ] `make lint` and `make test` pass.
