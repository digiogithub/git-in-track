---
id: GIT-T-0184
type: task
title: Document the engine flags, config keys and lifecycle
status: done
priority: medium
parent: GIT-US-0084
milestone: GIT-M-0011
author: mcp
labels: [docs]
estimate: 1
created: 2026-09-13T13:19:42Z
updated: 2026-09-13T16:20:25Z
started: 2026-09-13T15:16:09Z
closed: 2026-09-13T16:20:25Z
---

## Description

Document the four `gintrack serve` flags and the matching config keys in `docs/07-cli-and-api.md`, and add the sync engine to the background components listed in `docs/02-architecture.md` alongside the watcher, the committer and the tunnel, including its shutdown behaviour. Record the new component in `CHANGELOG.md`.

## Acceptance Criteria

- [x] Flags and config keys are documented with their defaults and precedence.
- [x] `docs/02-architecture.md` lists the engine as a background component with its shutdown contract.
- [x] `make lint` passes and `CHANGELOG.md` is updated.

## Notes

`docs/07-cli-and-api.md` is complete: §3.2 carries the `sync.engine` block of the configuration
file, §3.3 the four `GINTRACK_SYNC_*` variables, and §4.1 the flag table, the full
flag > environment > file > default statement, the `retention` key, the four registered job kinds
with their coalescing keys, and the bounded-drain shutdown contract. §5.5 states that
`PATCH /api/v1/sync/settings` answers `persisted: true` when the companion has a configuration file.
`CHANGELOG.md` has the entry.

`docs/02-architecture.md` now carries it too. There was no existing "background components" list to
append to, so the paragraph went into §3.2 under the companion-mode table, beside the three
components it belongs with: the engine is built by `server.New`, started in `Server.Start` on the
same path as the watcher, the committer and the tunnel, closed on the way out with a context
detached through `context.WithoutCancel`, drains for a bounded five-second grace period and
journals what is left, needs its handlers registered before `Start` because `Start` replays the
journal, and starts idle when no integration is configured. A "Background jobs" row was added to the
same table.

Two stale claims in §6 were corrected while there, because they were the opposite of true after
waves 3 and 4: `internal/syncengine` said "not reachable from any surface yet" and
`internal/youtrack` said "nothing imports it yet". Both now name their callers. The `internal/mcp`
row's "thirteen tools" was corrected to 7 read-only / 22 with writes.
