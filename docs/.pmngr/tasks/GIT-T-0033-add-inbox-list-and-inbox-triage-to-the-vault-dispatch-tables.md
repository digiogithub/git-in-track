---
id: GIT-T-0033
type: task
title: Add inbox.list and inbox.triage to the vault dispatch tables
status: done
priority: medium
parent: GIT-US-0056
milestone: GIT-M-0012
author: mcp
labels: [core]
estimate: 4
created: 2026-09-13T13:16:06Z
updated: 2026-09-13T14:40:36Z
started: 2026-09-13T14:40:04Z
closed: 2026-09-13T14:40:36Z
---

## Description

Add `inbox.list` and `inbox.triage` cases to `Vault.Dispatch` (`internal/vault/vault.go:320-404`) and to the workspace table (`internal/vault/dispatch.go:73-260`), implemented in a new `internal/vault/inbox.go`. `inbox.list` builds a `core.Filter` with `Inbox: only`, `SnoozeAsOf: v.Now()` and the caller's status filter. `inbox.triage` takes `{id, rev, action, status?, type?, parent?, snoozedUntil?, duplicateOf?}` and applies one action per call: accept via `FileStore.MoveWith` plus a type/parent patch, reject via a move to the cancelled status, snooze and duplicate via `FileStore.Update`. Duplicate also appends a `duplicates` link and writes the `duplicated_by` inverse on the target (`core.LinkKind.Inverse()`, `internal/core/model.go:126`). The whole action runs inside the existing vault mutex and produces one `WriteSet`.

## Acceptance Criteria

- [x] Both methods are dispatchable from the per-vault and workspace tables and work in the WASM build.
- [ ] Each action writes only the fields it owns; accept applies status, type and parent in a single rev-checked write with the workflow transition validated.
- [x] A stale rev is refused with `stale_revision` carrying `currentRev` and the conflicting fields.
- [x] `go test -race ./internal/vault/...` covers all four actions, the inverse link and the stale-rev path.

## Notes

The second criterion is ticked for status and parent and deliberately left unticked for
`type`: an item id encodes its type (`GIT-US-0056` is a story because of the `US` segment),
so changing the type would mean changing the id, which R-ID-3 forbids. `inbox.triage`
accepts a `type` argument and refuses it with `invalid_request` when it differs from the
item's own type, naming the way out — create the right type and mark this one `duplicate`.

Every action is one `FileStore.Update`, not `MoveWith`: `Update` already runs
`checkTransition` and `stampTransition` when the patch carries a status (`internal/core/store.go:455-472`),
which is what lets status, parent and the `inbox:` block travel in a single conditional write.
