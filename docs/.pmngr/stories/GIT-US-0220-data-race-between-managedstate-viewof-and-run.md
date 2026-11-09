---
id: GIT-US-0220
type: story
title: Managed Pando slot state is read without synchronisation (two flaky tests)
status: backlog
priority: medium
author: claude-code
labels: [server, agent-ok]
estimate: 1
created: 2026-11-09T13:00:00Z
updated: 2026-11-09T13:00:00Z
---

## Description

Bug, intermittent CI failure. `go test -race` failed on PR #152 on 2026-11-09 with a
`DATA RACE` in `internal/server`, detected during `TestManagedInstanceTakenByAnotherServe`:
`(*managedState).viewOf` (`internal/server/search_managed.go:557`) reads `slot.sup` without
holding `slot.mu`, while `(*managedState).run` (`search_managed.go:393`) replaces it under
`slot.mu` when another `gintrack serve` already owns the managed Pando instance. The race
detector then failed every other test of the package that was running at the time. A rerun
of the same commit passed. The PR itself touched only `internal/gitops` tests, so the race
is in `main`.

A second intermittent failure in the same code, on PR #165 the same day (a backlog-only
change): `TestManagedRegistersTheCodeProject` (`internal/server/search_managed_test.go:421`)
saw the code index status `""` instead of `"indexing"` and no code project registered. The
likely cause is the same: the slot's state is read before the goroutine started by `enable`
has published it, without waiting on or locking the slot.

## Acceptance Criteria

- [ ] Every read of `slot.sup` outside `run` holds `slot.mu` (or reads through an accessor that does).
- [ ] `TestManagedRegistersTheCodeProject` waits for the slot state it asserts (or the state is published before `enable` returns) instead of racing the goroutine.
- [ ] `go test -race -count=50 -run TestManaged ./internal/server` passes.
- [ ] No behaviour change in the managed Pando settings view.

## Notes

Recorded at the supervisor's request while merging the gintrack hub PoC (GIT-EP-0034).
