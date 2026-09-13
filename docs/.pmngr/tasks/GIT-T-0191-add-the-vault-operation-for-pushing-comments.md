---
id: GIT-T-0191
type: task
title: Add the vault operation for pushing comments
status: done
priority: medium
parent: GIT-US-0079
milestone: GIT-M-0011
author: mcp
labels: [core, server, agent-ok]
estimate: 2
created: 2026-09-13T13:19:57Z
updated: 2026-09-13T15:34:13Z
started: 2026-09-13T15:34:01Z
closed: 2026-09-13T15:34:13Z
---

## Description

Add `youtrack.comment.push` to the vault dispatch table (`internal/vault/vault.go:320-404`) taking `{project, itemId, commentPath?, all?}`, validating the arguments, checking the project link, and enqueueing one push job per selected comment. It returns `{jobId, pushed[], skipped[], failed[]}` so both the MCP tool and the CLI can report without re-deriving anything.

## Acceptance Criteria

- [x] The method exists in the dispatch table and validates its arguments with field-level errors.
- [x] `all: true` selects every not-yet-pushed comment of the item; `commentPath` selects one.
- [x] The result carries pushed, skipped and failed lists.
- [x] `go test -race ./internal/vault/...` covers both selection modes and the unlinked-item error.

## Notes

Landed in `internal/vault/youtrackpush.go`, routed per project by
`Workspace.routeItem` (the router reads `id`, and this method spells its item
`itemId`).

`pushed[]` is what was *queued*, not what has already arrived upstream: nothing
is pushed inline, so a comment saved in the UI never waits on YouTrack. A
comment that already carries a YouTrack reference is reported in `skipped[]`
with its remote id; a comment the engine refused lands in `failed[]`.

An item with no YouTrack `external` reference fails with `invalid_request` and a
message that says to import or link the item first — non-retryable on purpose,
since no amount of waiting creates the issue to comment on.
