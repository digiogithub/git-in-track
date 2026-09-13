---
id: GIT-T-0224
type: task
title: Add the comment.update vault method for rev-guarded comment rewrites
status: done
priority: medium
parent: GIT-US-0068
milestone: GIT-M-0011
author: mcp
labels: [core, agent-ok]
estimate: 2
created: 2026-09-13T16:20:19Z
updated: 2026-09-13T16:20:31Z
started: 2026-09-13T16:20:23Z
closed: 2026-09-13T16:20:31Z
---

## Description

`comment.add` appended and nothing updated, so the YouTrack comment push had to open a comment file, hash it, re-read it and write it back itself, then rebuild the index by hand (`writeCommentExternal` in `internal/server/youtrackcomments.go`, flagged on GIT-T-0158). That is a layering break: the vault owns the index and the write set, and a writer that goes around it owns neither.

Add `comment.update`: a sparse, rev-guarded patch of one comment file that goes through the normal `WriteSet` and commit path like every other vault write.

## Acceptance Criteria

- [x] `comment.update` patches a comment file under an optimistic lock and refuses a stale rev with `stale_revision` carrying `currentRev`.
- [x] It writes through the tracking file system, so the answer carries the `WriteSet` commit-on-save and the browser host persist, and the index is folded forward by the same commit.
- [x] It can record an external reference without touching the comment text, and upserts one entry per system so a re-delivered push replaces the reference instead of appending.
- [x] `go test -race ./internal/vault/...` covers the write-back, the stale-rev path and the refusals.

## Notes

Landed as `internal/vault/commentedit.go` and `internal/vault/commentedit_test.go`.

Params: `path` (required, the vault-relative comment file — a comment has no id
of its own, its file name is its identity), `rev` (required; `"*"` is the
If-Match wildcard), optional `id` cross-checked against the comment's own `item`
field, and the patch: `body`, `external` (replaces the whole list) and
`setExternal` (upserts by *system*). It routes by `id` through the workspace
router, and falls back to the default repository when `id` is absent.

Two decisions a reader would not guess, both pinned by tests: `updated` moves
only when the *text* changes — recording a remote comment id is bookkeeping, not
an edit a reader should see stamped — and an empty body is refused, because
removing a comment is removing its file, not blanking it. There is deliberately
no `comment.delete`.

Still to do elsewhere: `internal/server` should delete `writeCommentExternal`
and its hand-rolled reindex and call this instead, and the CoreApi method
catalogue in `docs/07-cli-and-api.md` should list it.
