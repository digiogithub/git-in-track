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
updated: 2026-09-13T16:03:03Z
started: 2026-09-13T15:16:09Z
---

## Description

Document the four `gintrack serve` flags and the matching config keys in `docs/07-cli-and-api.md`, and add the sync engine to the background components listed in `docs/02-architecture.md` alongside the watcher, the committer and the tunnel, including its shutdown behaviour. Record the new component in `CHANGELOG.md`.

## Acceptance Criteria

- [x] Flags and config keys are documented with their defaults and precedence.
- [ ] `docs/02-architecture.md` lists the engine as a background component with its shutdown contract.
- [x] `make lint` passes and `CHANGELOG.md` is updated.

## Notes

`docs/07-cli-and-api.md` is complete now: §3.2 carries the `sync.engine` block
of the configuration file, §3.3 the four `GINTRACK_SYNC_*` variables, and §4.1
the flag table, the **full** flag > environment > file > default statement, the
`retention` key, the four registered job kinds with their coalescing keys, and
the bounded-drain shutdown contract. §5.5 now states that
`PATCH /api/v1/sync/settings` answers `persisted: true` when the companion has a
configuration file. `CHANGELOG.md` has the entry.

`docs/02-architecture.md` is **still not touched** — every file under `docs/`
except `07-cli-and-api.md` belongs to another agent this wave — so that
criterion stays unticked and this task stays in review. The paragraph it needs
is one sentence long: the engine is a background component beside the watcher,
the committer and the tunnel; it is started in `Server.Start` and closed on the
same path with `context.WithoutCancel`; shutdown drains for a bounded grace
period and journals whatever is left; with no integration configured it starts
idle.
