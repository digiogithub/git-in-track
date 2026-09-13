---
id: GIT-T-0037
type: task
title: Create items directly into the inbox through item.create
status: todo
priority: medium
parent: GIT-US-0056
milestone: GIT-M-0012
author: mcp
labels: [core, agent-ok]
estimate: 2
created: 2026-09-13T13:16:14Z
updated: 2026-09-13T13:16:14Z
---

## Description

Add an `Inbox *InboxDraft` option to `core.ItemDraft` (`internal/core/store.go:118`) and honour it in `FileStore.Create` (`:321`): the item is created with the project's triage status (`Workflow.TriageStatus()`), stamped `inbox: {status: pending, source, received}` and refused with a clear error when the project declares no triage status. Thread the option through `item.create` in `internal/vault/vault.go` so every existing caller keeps working unchanged when it is absent.

## Acceptance Criteria

- [ ] `item.create` with the inbox option produces a pending triage item with the requested `source`; without it nothing changes.
- [ ] Creating into the inbox of a project with no triage status fails with a named error, not a panic or a silent backlog write.
- [ ] `applyDefaults` does not override the triage status.
- [ ] `go test -race ./internal/core/... ./internal/vault/...` covers both paths.
