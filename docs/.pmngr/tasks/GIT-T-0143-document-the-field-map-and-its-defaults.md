---
id: GIT-T-0143
type: task
title: Document the field map and its defaults
status: todo
priority: medium
parent: GIT-US-0065
milestone: GIT-M-0011
author: mcp
labels: [docs, agent-ok]
estimate: 1
created: 2026-09-13T13:18:46Z
updated: 2026-09-13T13:18:46Z
---

## Description

Document the `integrations.youtrack.field_map` shape and its built-in defaults in `docs/03-data-model.md` §6, the discovery endpoint in `docs/07-cli-and-api.md`, and the settings section in `docs/05-web-app.md`. Add a `CHANGELOG.md` entry.

## Acceptance Criteria

- [ ] The `field_map` shape and defaults are documented in `docs/03-data-model.md` §6.
- [ ] The endpoint and the settings section are documented in `docs/07-cli-and-api.md` and `docs/05-web-app.md`.
- [ ] `CHANGELOG.md` has an entry and `make lint` passes.
