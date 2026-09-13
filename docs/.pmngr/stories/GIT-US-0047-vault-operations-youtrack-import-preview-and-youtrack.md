---
id: GIT-US-0047
type: story
title: Vault operations youtrack.import.preview and youtrack.import.run
status: backlog
priority: high
parent: GIT-EP-0012
milestone: GIT-M-0011
author: mcp
labels: [core, server]
estimate: 8
created: 2026-09-13T13:11:28Z
updated: 2026-09-13T13:11:28Z
---

## Description

As a user importing issues, I want the import itself to be a core API operation, so that REST, MCP, the CLI and the web app all get the same idempotent behaviour, the same validation and the same git commits without any of them re-implementing it.

Add two methods to the string-dispatch table in `internal/vault` — `youtrack.import.preview` and `youtrack.import.run` — as cases in `Vault.Dispatch` (`internal/vault/vault.go:320-404`) and, where the call needs workspace routing, in `internal/vault/dispatch.go:73`. `preview` resolves each requested issue through the YouTrack client and the mapping package and returns a per-issue plan `{youtrackId, title, mappedType, action: create|update, targetId?, parent?, milestone?, warnings[]}` without writing anything. `run` performs the same resolution and then writes through `core.FileStore.Create` (`internal/core/store.go:321`) and `Update` (`:423`), collecting the `WriteSet` via `v.fs.begin()` / `v.commit(ctx)` (`vault.go:1511`) so the import lands as ordinary commits, index updates and WebSocket events.

Idempotency is the pair `(external.system, external.id)`: before writing, look the item up by that pair through the index; when found, update it in place with an `ItemPatch` and keep its gintrack id, otherwise create a new one. Subtask recursion is bounded by a `depth` option (0 means the selected issues only) and must be cycle-safe — a visited set keyed on `idReadable`. Parent and `links[]` targets that resolve to issues outside the import set are recorded as warnings rather than dangling references. Comments are written as comment files per ADR-012 with the original author and timestamp when `includeComments` is set, skipping any comment whose YouTrack id is already present. The result is a per-issue list `{youtrackId, itemId, action, error?}` so a partial failure is reportable instead of aborting the batch.

## Acceptance Criteria

- [ ] `youtrack.import.preview` and `youtrack.import.run` are cases in the vault dispatch table and are reachable from the workspace layer.
- [ ] Params accept `{project, query|ids[], depth, includeLinks, includeComments, includeAttachments}` and are validated with field-level errors.
- [ ] Lookup by `(external.system, external.id)` decides create vs update; an update keeps the existing gintrack id and does not duplicate.
- [ ] Subtask recursion honours `depth`, is cycle-safe and records out-of-set parents and link targets as warnings.
- [ ] Comments are written per ADR-012 with original author and timestamp; already-imported comment ids are skipped.
- [ ] Writes go through `FileStore.Create`/`Update` and one `WriteSet` commit, producing `item.changed` and `index.updated` events like any other write.
- [ ] `run` returns a per-issue result list and does not abort the whole batch on a single failure.
- [ ] `go test -race ./internal/vault/...` covers create, re-import update, depth recursion and a failing issue, using a fake YouTrack client.

## Notes

Depends on GIT-EP-0011 (`external` field, `internal/youtrack` client, `project.yaml` link) and on the mapping story of this epic. The long-running batched execution is GIT-EP-0015's sync engine — these vault methods stay synchronous and are what the job handler calls.

Existing code to follow: `internal/vault/vault.go:304-405` (dispatch), `:1170`/`:1185` (`comment.list`/`comment.add`), `internal/vault/wire.go:56` (`WriteSet`). `ItemDraft.ID` (`internal/core/store.go:152`) exists for importers but pins a gintrack id, not a foreign one.

Do NOT put mapping or HTTP logic in `internal/server` or `internal/mcp`. Do NOT hold the vault mutex across network calls — resolve remote data first, then write.
