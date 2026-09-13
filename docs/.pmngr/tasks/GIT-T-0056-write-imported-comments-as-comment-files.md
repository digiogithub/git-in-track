---
id: GIT-T-0056
type: task
title: Write imported comments as comment files
status: done
priority: medium
parent: GIT-US-0047
milestone: GIT-M-0011
author: mcp
labels: [core, server, agent-ok]
estimate: 2
created: 2026-09-13T13:16:42Z
updated: 2026-09-13T15:09:07Z
started: 2026-09-13T15:08:54Z
closed: 2026-09-13T15:09:07Z
---

## Description

When `includeComments` is set, write the mapped comment drafts through the same path `comment.add` uses (`internal/vault/vault.go:1185`) so they land at `<docs>/.pmngr/comments/<ITEM-ID>/<timestamp>-<author>.md` per ADR-012, using the original author and `created` for the file name and honouring the `-2`, `-3` collision suffixes. Skip any comment whose YouTrack id already appears in an existing comment's `external` block so a re-import does not duplicate the thread.

## Acceptance Criteria

- [x] Comments land at the ADR-012 path with the original author and timestamp and handle name collisions.
- [x] A comment already present by YouTrack id is skipped on re-import.
- [x] The comment writes are part of the same `WriteSet` as the item write.
- [x] `go test -race ./internal/vault/...` covers first import and re-import of the same thread.

## Notes

`youtrackComments` maps the thread with `mapping.CommentsToDrafts` and writes each draft through `core.FileStore.AddComment`, which is the same path `comment.add` uses, so the ADR-012 file name, the original author and the original `created` and the `-2`/`-3` collision suffixes all come from the core.

One wrinkle a reviewer should know: `core.CommentDraft` has no `external` field, while `core.Comment` does, so the created comment file is immediately rewritten once through the vault's tracking file system with `external` and `updated` set from `mapping.CommentDraft`. Without that reference a re-import could not tell an imported comment from a new one. `youtrackCommentIDs` reads the existing thread and skips every YouTrack comment id already recorded, which is what makes the second import write nothing. All of it happens between the batch's single `begin`/`commit`, so the comment files are part of the same `WriteSet` as the item.
