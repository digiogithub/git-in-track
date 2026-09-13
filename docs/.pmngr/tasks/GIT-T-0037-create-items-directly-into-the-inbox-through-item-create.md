---
id: GIT-T-0037
type: task
title: Create items directly into the inbox through item.create
status: done
priority: medium
parent: GIT-US-0056
milestone: GIT-M-0012
author: mcp
labels: [core, agent-ok]
estimate: 2
created: 2026-09-13T13:16:14Z
updated: 2026-09-13T14:40:43Z
started: 2026-09-13T14:40:05Z
closed: 2026-09-13T14:40:43Z
---

## Description

Add an `Inbox *InboxDraft` option to `core.ItemDraft` (`internal/core/store.go:118`) and honour it in `FileStore.Create` (`:321`): the item is created with the project's triage status (`Workflow.TriageStatus()`), stamped `inbox: {status: pending, source, received}` and refused with a clear error when the project declares no triage status. Thread the option through `item.create` in `internal/vault/vault.go` so every existing caller keeps working unchanged when it is absent.

## Acceptance Criteria

- [x] `item.create` with the inbox option produces a pending triage item with the requested `source`; without it nothing changes.
- [x] Creating into the inbox of a project with no triage status fails with a named error, not a panic or a silent backlog write.
- [x] `applyDefaults` does not override the triage status.
- [x] `go test -race ./internal/core/... ./internal/vault/...` covers both paths.

## Notes

The core half was already on `main` from the previous wave: `ItemDraft.Inbox` and its
handling in `FileStore.Create` exist, and `applyDefaults` only fills a status that is empty,
so a forced triage status survives it untouched.

What landed here is the vault half, so that `internal/core` needed no edit: the `inbox`
option of `item.create` is decoded in `internal/vault/wire.go` and folded into the draft by
`(*Vault).inboxDraft` (`internal/vault/inbox.go`), which forces the project's triage status,
stamps `{status: pending, source, received}` and refuses a project with no triage status with
the code `no_triage_status`. Putting the refusal in the vault keeps the message specific — it
names the project and `project.yaml` — and keeps the check where the project configuration
already is.
