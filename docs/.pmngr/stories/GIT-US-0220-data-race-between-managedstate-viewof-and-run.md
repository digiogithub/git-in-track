---
id: GIT-US-0220
type: story
title: Data race between managedState.viewOf and run on slot.sup
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

## Acceptance Criteria

- [ ] Every read of `slot.sup` outside `run` holds `slot.mu` (or reads through an accessor that does).
- [ ] `go test -race -count=50 -run TestManaged ./internal/server` passes.
- [ ] No behaviour change in the managed Pando settings view.

## Notes

Recorded at the supervisor's request while merging the gintrack hub PoC (GIT-EP-0034).
