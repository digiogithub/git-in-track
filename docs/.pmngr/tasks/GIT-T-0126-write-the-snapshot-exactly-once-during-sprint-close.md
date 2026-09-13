---
id: GIT-T-0126
type: task
title: Write the snapshot exactly once during sprint.close
status: done
priority: medium
parent: GIT-US-0080
milestone: GIT-M-0012
author: mcp
labels: [core, server]
estimate: 3
created: 2026-09-13T13:18:23Z
updated: 2026-09-13T14:57:41Z
started: 2026-09-13T14:57:30Z
closed: 2026-09-13T14:57:41Z
---

## Description

Extend `Workspace.CloseSprint` (`internal/vault/sprint.go:561-600`) so that, before any carry decision is applied, it builds the snapshot from the current view and metrics and writes it into the sprint file in the same rev-checked write that sets `state: closed`. A sprint that already carries a snapshot keeps it: closing again never recomputes. Where no history source is installed (browser-only mode) the snapshot is still written, marked approximate by its provenance.

## Acceptance Criteria

- [x] Closing writes `state: closed` and the snapshot in one write, before any item is carried.
- [x] Re-closing a sprint leaves the existing snapshot untouched.
- [x] A close in browser-only mode produces a snapshot whose provenance says `approximate`.
- [x] `go test -race ./internal/vault/...` covers all three cases.

## Notes

The snapshot is computed right after `core.SummarizeClose` and before `applyCarries`, from `w.reconstruct` + `core.BuildSprintMetrics` + `core.BuildSprintSnapshot`, and assigned to the sprint immediately before the single rev-checked `WriteSprint` that sets `state: closed`. It is skipped for a dry run (which writes nothing) and for a sprint that already carries one. Covered by `TestSprintCloseFreezesTheSnapshot` in `internal/vault/metrics_test.go`; the docs/04 §8.2 "As built" paragraph was deleted.
