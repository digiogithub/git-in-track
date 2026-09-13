---
id: GIT-T-0056
type: task
title: Write imported comments as comment files
status: todo
priority: medium
parent: GIT-US-0047
milestone: GIT-M-0011
author: mcp
labels: [core, server, agent-ok]
estimate: 2
created: 2026-09-13T13:16:42Z
updated: 2026-09-13T13:16:42Z
---

## Description

When `includeComments` is set, write the mapped comment drafts through the same path `comment.add` uses (`internal/vault/vault.go:1185`) so they land at `<docs>/.pmngr/comments/<ITEM-ID>/<timestamp>-<author>.md` per ADR-012, using the original author and `created` for the file name and honouring the `-2`, `-3` collision suffixes. Skip any comment whose YouTrack id already appears in an existing comment's `external` block so a re-import does not duplicate the thread.

## Acceptance Criteria

- [ ] Comments land at the ADR-012 path with the original author and timestamp and handle name collisions.
- [ ] A comment already present by YouTrack id is skipped on re-import.
- [ ] The comment writes are part of the same `WriteSet` as the item write.
- [ ] `go test -race ./internal/vault/...` covers first import and re-import of the same thread.
