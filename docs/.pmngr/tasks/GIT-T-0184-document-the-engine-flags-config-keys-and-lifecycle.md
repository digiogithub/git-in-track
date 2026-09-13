---
id: GIT-T-0184
type: task
title: Document the engine flags, config keys and lifecycle
status: in_review
priority: medium
parent: GIT-US-0084
milestone: GIT-M-0011
author: mcp
labels: [docs]
estimate: 1
created: 2026-09-13T13:19:42Z
updated: 2026-09-13T15:16:46Z
started: 2026-09-13T15:16:09Z
---

## Description

Document the four `gintrack serve` flags and the matching config keys in `docs/07-cli-and-api.md`, and add the sync engine to the background components listed in `docs/02-architecture.md` alongside the watcher, the committer and the tunnel, including its shutdown behaviour. Record the new component in `CHANGELOG.md`.

## Acceptance Criteria

- [x] Flags and config keys are documented with their defaults and precedence.
- [ ] `docs/02-architecture.md` lists the engine as a background component with its shutdown contract.
- [x] `make lint` passes and `CHANGELOG.md` is updated.

## Notes

`docs/07-cli-and-api.md` §4.1 gained the four flags in the usage block and a
"The background job engine" subsection: a table of flag, environment variable,
default and range, the precedence statement, where the journal is written, the
bounded-drain shutdown contract and the handler idempotence requirement. The
configuration-file keys are documented as **absent**, because they are: see
GIT-T-0175.

`docs/02-architecture.md` was **not touched** — every file under `docs/` except
`07-cli-and-api.md` belongs to another agent this wave — so that criterion is
left unticked. The paragraph it needs is the shutdown contract above: the engine
is a background component beside the watcher, the committer and the tunnel, it
drains for a bounded grace period on a context detached from the shutdown, and
it journals whatever is left.
