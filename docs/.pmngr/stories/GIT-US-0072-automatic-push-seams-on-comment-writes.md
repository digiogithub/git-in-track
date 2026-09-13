---
id: GIT-US-0072
type: story
title: Automatic push seams on comment writes
status: done
priority: medium
parent: GIT-EP-0013
milestone: GIT-M-0011
author: mcp
labels: [server, mcp]
estimate: 5
created: 2026-09-13T13:13:38Z
updated: 2026-09-13T17:08:52Z
started: 2026-09-13T15:41:29Z
closed: 2026-09-13T17:08:52Z
---

## Description

As a team with `push_comments: auto`, I want every comment written on a linked item to be queued for YouTrack automatically, so that nobody has to remember to press a button and the conversation stays in one place.

Add the enqueue seam in exactly two places and nowhere else. First, after `comment.add` commits in `internal/vault/vault.go:1185` the companion learns of the new comment through the `WriteSet` and the existing `publishWrite` path (`internal/server/events.go:256`); the server inspects the write set, and when the project's `integrations.youtrack.push_comments` is `auto` and the parent item carries a YouTrack `external` reference, it enqueues a `youtrack.comment.push` job. Second, MCP writes already fan out through `Options.AfterWrite` to `s.publishAgentWrite` (`internal/server/mcp.go:87`, `:260`), so an agent-written comment reaches the same seam without a second implementation.

The enqueue must be debounced and coalesced per comment path so a rapid edit produces one push, and it must use `context.WithoutCancel` so a closing HTTP response cannot cancel it. Comments the push itself writes back (the `external` id write) must not re-trigger a push — guard on the write set's changed fields, not on a flag. When `push_comments` is `manual` (the default) nothing is enqueued and the manual action from the UI story is the only trigger. A project that is not linked never enqueues, and the decision is logged once per item, not per comment.

## Acceptance Criteria

- [ ] Comments created through REST, the web app and MCP all reach one enqueue seam; there is no duplicated logic per surface.
- [ ] `push_comments: auto` enqueues a push for a comment on a linked item; `manual` enqueues nothing.
- [ ] Enqueue is coalesced per comment path and uses `context.WithoutCancel`.
- [ ] The write-back of the YouTrack comment id does not re-trigger a push (no feedback loop).
- [ ] Comments on unlinked items and in unlinked projects are ignored without an error.
- [ ] `go test -race ./internal/server/...` covers auto and manual modes, coalescing and the no-loop guard with a fake engine.

## Notes

Depends on the `external`-on-comments story of this epic, on GIT-EP-0015 for `Engine.Enqueue`, and on GIT-EP-0011 for the `push_comments` setting in `project.yaml`.

Seams to reuse: `internal/vault/vault.go:1185` (`comment.add`), `internal/vault/wire.go:56` (`WriteSet`), `internal/server/events.go:256` (`publishWrite`), `internal/server/mcp.go:87` (`AfterWrite`). Coalescing shape: `internal/gitops/committer.go:157-211` (`Enqueue`, coalesce key, `arm`, `fire`).

Do NOT call YouTrack from inside the vault mutex or from `internal/core` — the core compiles to WASM and must stay free of network code. Do NOT add a new hook into `internal/core`.
