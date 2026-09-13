---
id: GIT-T-0032
type: task
title: Write ADR-032 and revise the no-credentials promise
status: todo
priority: medium
parent: GIT-US-0048
milestone: GIT-M-0011
author: mcp
labels: [docs, security]
estimate: 2
created: 2026-09-13T13:16:04Z
updated: 2026-09-13T13:16:04Z
---

## Description

Write `docs/adr/ADR-032-local-credential-storage.md` stating exactly which credential is now stored, where, with what permissions, how it can be supplied instead by environment variable, and what browser-only mode does (memory for the session only, never `localStorage`). Revise the statement at `docs/10-development-guidelines.md:707-711` so it describes the new reality rather than contradicting it, document the `project.yaml` block in `docs/03-data-model.md` §6 and the config key and env var in `docs/07-cli-and-api.md` §3, and add a `CHANGELOG.md` entry.

## Acceptance Criteria

- [ ] ADR-032 follows the template in `docs/adr/README.md` with a filled-in negative consequences section.
- [ ] `docs/10-development-guidelines.md` no longer claims that no credential is ever stored, and points to ADR-032.
- [ ] `docs/03-data-model.md` §6 and `docs/07-cli-and-api.md` §3 are updated and `CHANGELOG.md` records the change.
