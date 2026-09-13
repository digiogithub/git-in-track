---
id: GIT-T-0226
type: task
title: Land imported issues in the inbox when the project asks for it
status: done
priority: medium
parent: GIT-US-0047
milestone: GIT-M-0011
author: mcp
labels: [core, server, agent-ok]
estimate: 2
created: 2026-09-13T17:29:34Z
updated: 2026-09-13T17:35:47Z
started: 2026-09-13T17:35:27Z
closed: 2026-09-13T17:35:47Z
---

## Description

`integrations.youtrack.land_in_inbox` decodes, validates and defaults to false, and `core.InboxLandingStatus` already answers the one question it asks, but nothing in the import write path reads either. An issue imported into a project that asked for triage still arrives straight in the backlog, which is the outcome the option exists to prevent.

Carry the setting into `vault.YouTrackLink`, populate it in `youtrackLinkOf` in `internal/server` as the other fields of the block are populated, and apply it where the importer builds a draft for an issue it is **creating**: the status comes from `core.InboxLandingStatus(cfg, link.LandInInbox)` and the draft gains an `inbox` block whose `status` is pending and whose `source` is `youtrack`. An issue being **updated** keeps whatever status it has; landing is a decision about arrival, not about every later sync.

`InboxLandingStatus` returns `ErrNoTriageStatus` when the project declares no triage status. That must fail the import rather than fall back to the initial status, for the reason its doc comment gives: "put these somewhere for review" and "this project has no place to review them" is a configuration mistake, and quietly landing a thousand issues in the backlog is the one behaviour nobody wants.

This task was missed when GIT-EP-0012 was planned: GIT-T-0078 covered the configuration half and its story assigned this half here, but no task was ever written for it. Found by the closing verification pass.

## Acceptance Criteria

- [x] `vault.YouTrackLink` carries the setting and `internal/server` populates it from `config.YouTrackLink`.
- [x] A created item lands in the project's triage status with `inbox.status: pending` and `inbox.source: youtrack` when the option is on, and in the initial status when it is off.
- [x] An updated item's status is untouched by the option.
- [x] A project with the option on and no triage status fails the import with a clear error rather than landing anything in the backlog.
- [x] `go test -race ./internal/vault/... ./internal/server/...` covers all four cases.

## Notes

`core.InboxLandingStatus` is at `internal/core/inbox.go:279` and already carries the reasoning. `core.ItemDraft.Inbox` exists (`internal/core/store.go:146`). The draft is built in the `else` branch of the decision path in `internal/vault/youtrack.go`, where `decision.status` is taken from `draft.Status`.

Do not reach for `MoveWith` or a second write: the status belongs on the draft before it is created.

### What landed

`core.InboxLandingStatus` needed no change. `vault.YouTrackLink` gained `LandInInbox`, `youtrackLinkOf` populates it, and `youtrackPlan` gained a `landing` status resolved once per import by the new `Vault.youtrackLanding`, under the vault lock, before the first write. `youtrackDecide` forces that status and the `inbox` block onto the draft in the create branch only; the update branch is untouched.

Two decisions worth reviewing. First, the refusal is raised for the **preview** as well as the run, with the existing `no_triage_status` code and a message naming the project and `project.yaml`: a preview that silently promised a backlog landing would be the same mistake one screen earlier. Second, the landing status is only applied when the option is on, so an import that did not ask for triage keeps the status the field map produced — applying `InboxLandingStatus`'s "off" answer unconditionally would have overwritten every mapped state with the initial status.
