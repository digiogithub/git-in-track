---
id: GIT-T-0126
type: task
title: Write the snapshot exactly once during sprint.close
status: todo
priority: medium
parent: GIT-US-0080
milestone: GIT-M-0012
author: mcp
labels: [core, server]
estimate: 3
created: 2026-09-13T13:18:23Z
updated: 2026-09-13T13:18:23Z
---

## Description

Extend `Workspace.CloseSprint` (`internal/vault/sprint.go:561-600`) so that, before any carry decision is applied, it builds the snapshot from the current view and metrics and writes it into the sprint file in the same rev-checked write that sets `state: closed`. A sprint that already carries a snapshot keeps it: closing again never recomputes. Where no history source is installed (browser-only mode) the snapshot is still written, marked approximate by its provenance.

## Acceptance Criteria

- [ ] Closing writes `state: closed` and the snapshot in one write, before any item is carried.
- [ ] Re-closing a sprint leaves the existing snapshot untouched.
- [ ] A close in browser-only mode produces a snapshot whose provenance says `approximate`.
- [ ] `go test -race ./internal/vault/...` covers all three cases.
