---
id: GIT-T-0184
type: task
title: Document the engine flags, config keys and lifecycle
status: todo
priority: medium
parent: GIT-US-0084
milestone: GIT-M-0011
author: mcp
labels: [docs]
estimate: 1
created: 2026-09-13T13:19:42Z
updated: 2026-09-13T13:19:42Z
---

## Description

Document the four `gintrack serve` flags and the matching config keys in `docs/07-cli-and-api.md`, and add the sync engine to the background components listed in `docs/02-architecture.md` alongside the watcher, the committer and the tunnel, including its shutdown behaviour. Record the new component in `CHANGELOG.md`.

## Acceptance Criteria

- [ ] Flags and config keys are documented with their defaults and precedence.
- [ ] `docs/02-architecture.md` lists the engine as a background component with its shutdown contract.
- [ ] `make lint` passes and `CHANGELOG.md` is updated.
