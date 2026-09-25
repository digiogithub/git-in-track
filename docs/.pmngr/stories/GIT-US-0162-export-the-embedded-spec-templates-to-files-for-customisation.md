---
id: GIT-US-0162
type: story
title: Export the embedded spec templates to files for customisation
status: in_progress
priority: medium
parent: GIT-EP-0023
milestone: GIT-M-0015
assignees: [claude]
author: claude
labels: [core, cli, web]
estimate: 3
created: 2026-09-25T08:00:00Z
updated: 2026-09-25T11:15:00Z
started: 2026-09-25T11:15:00Z
links:
  - { kind: relates_to, target: GIT-US-0111 }
---

## Description

GIT-US-0111 (PR #69) ships the spec and requirement templates embedded in the binary. The maintainer decided on 2026-09-25 that the templates stay embedded by default, but a team can extract them to files and customise them. A file on disk, when present, wins over the embedded copy.

This adds a template location to the backlog layout, which is a data-model change. It needs a short ADR, or an amendment to ADR-037, and a docs/03 update before the code, as the rules in AGENTS.md require.

## Acceptance Criteria

- [ ] ADR (or ADR-037 amendment) and docs/03: the override location (for example `<docs>/.pmngr/templates/{spec,requirement}.md`), precedence (file over embedded), and what happens when a file is invalid (lint warning, embedded fallback).
- [ ] `gintrack spec templates export [--force] [--dry-run] [--json]` writes the embedded templates to that location. It never overwrites an edited file without `--force`.
- [ ] The core loads the override through the FS it is given, so it stays WASM-clean. Spec creation, `create_requirement`, the web editor and the Add-requirement dialog all use the override when present, in both companion and browser-only modes.
- [ ] `gintrack doctor` lints the override templates.
- [ ] Tests cover the default embedded path, the override, an invalid override falling back, and export idempotence. `make test`, `make lint` and `make wasm` pass.
