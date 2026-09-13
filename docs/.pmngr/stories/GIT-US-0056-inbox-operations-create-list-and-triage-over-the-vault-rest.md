---
id: GIT-US-0056
type: story
title: "Inbox operations: create, list and triage over the vault, REST and MCP"
status: done
priority: high
parent: GIT-EP-0016
milestone: GIT-M-0012
author: mcp
labels: [core, server, mcp]
estimate: 8
created: 2026-09-13T13:12:15Z
updated: 2026-09-13T15:18:04Z
started: 2026-09-13T14:42:12Z
closed: 2026-09-13T15:18:04Z
---

## Description

As anyone or anything with access to a project, I want to drop an item into its inbox and later accept, reject, snooze or mark it duplicate in one rev-checked call, so that triage is a single auditable write rather than a sequence of edits a client has to orchestrate.

The business logic belongs in `internal/vault`. Two new dispatch methods join the string table at `internal/vault/vault.go:320-404`: `inbox.list` (a `core.Filter` pre-set to `Inbox: only`, with `SnoozeAsOf` defaulted to `Vault.Now()`) and `inbox.triage`. `item.create` gains an `inbox` option on `ItemDraft` that forces the project's triage status and stamps `inbox: {status: pending, source, received}`. `inbox.triage` takes `{id, rev, action, status?, type?, parent?, snoozed_until?, duplicate_of?}` and applies exactly one action inside the vault mutex: **accept** moves the item to the requested non-triage status through `FileStore.MoveWith` (so `checkTransition` and `stampTransition` run) and may change `type` and `parent` in the same patch, then sets `inbox.status: accepted`; **reject** sets `inbox.status: rejected` and moves the item to the project's cancelled status; **snooze** sets `inbox.status: snoozed` + `snoozed_until` and leaves the status alone; **duplicate** sets `inbox.status: duplicate`, `duplicate_of` and appends a `duplicates` link, with the `duplicated_by` inverse written on the target (`core.LinkKind.Inverse()`, `internal/core/model.go:126`). Every action is refused with `stale_revision` when the quoted rev is not current.

The server layer is thin, as everywhere else: `internal/server/items.go` mounts `GET /api/v1/inbox` and `POST /api/v1/items/{id}/triage` (If-Match required) and forwards through `s.call(...)` (`internal/server/api.go:134-156`). WS events reuse the existing publishers (`internal/server/events.go:256` `publishWrite`) plus a new `inbox.changed` topic carrying `{project, id, action, pendingCount}` so an open inbox pane and the sidebar badge update without a refetch. MCP gets three tools in a new `internal/mcp/tools_inbox.go`: `create_inbox_item` (write), `list_inbox` (read, untrusted) and `triage_inbox_item` (write), registered from `registerTools` (`internal/mcp/tools.go:24`) and added to the surface-pinning tests.

## Acceptance Criteria

- [x] `inbox.list` and `inbox.triage` exist in both dispatch tables (`internal/vault/vault.go:320`, `internal/vault/dispatch.go:73`) and work in the WASM build as well as the companion.
- [ ] Accept applies status, and optionally type and parent, in a single rev-checked write; the transition is validated by the project workflow and `started`/`closed` are stamped as usual.
- [x] Reject, snooze and duplicate each write exactly the fields their action owns; duplicate also writes the `duplicates` link and its `duplicated_by` inverse on the target.
- [x] A triage call quoting a stale rev is refused with `stale_revision` carrying `currentRev` and the conflicting fields; `rev: "*"` still waives.
- [ ] `GET /api/v1/inbox?status=&project=&cursor=` paginates like the other listings and `POST /api/v1/items/{id}/triage` requires `If-Match` and answers 412 on a mismatch.
- [ ] A `inbox.changed` WS event is published on every triage and on every inbox create, documented in `docs/07-cli-and-api.md` §WS topics.
- [x] MCP exposes `create_inbox_item`, `list_inbox` and `triage_inbox_item`; the write tools are hidden entirely on a read-only server, and `TestToolSurface` (`internal/mcp/tools_test.go:26`) is updated.
- [ ] `go test -race ./internal/vault/... ./internal/server/... ./internal/mcp/...` covers each action, the stale-rev path and the snooze-expiry listing.

## Notes

Existing code to follow: `internal/vault/vault.go:304-405` (dispatch), `:1511` (`commit`/`WriteSet`), `internal/core/store.go:321` (`Create`), `:423` (`Update`), `:495` (`Move`/`MoveWith`), `internal/server/items.go:19-36` (routes), `internal/server/api.go:172-189` (`ifMatch`/`requireIfMatch`), `internal/mcp/tools.go:31-98` (`toolDef` + `register`) and the checklist at report section 5.5.

Imported and agent-submitted inbox bodies are untrusted third-party content: `list_inbox` and `get_item` must keep the `Untrusted: true` annotation (`internal/mcp/tools.go:52`).

Do NOT put HTTP or business logic in `internal/mcp` or `internal/server` — both are adapters over `Vault.Dispatch`. Do NOT split a triage action into two writes (doc 03 R-REV-3c). Do NOT delete the item on reject: rejection is a status, soft delete stays a separate operation (ADR-026).
