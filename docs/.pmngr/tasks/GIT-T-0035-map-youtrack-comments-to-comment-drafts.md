---
id: GIT-T-0035
type: task
title: Map YouTrack comments to comment drafts
status: todo
priority: medium
parent: GIT-US-0045
milestone: GIT-M-0011
author: mcp
labels: [core, agent-ok]
estimate: 1
created: 2026-09-13T13:16:11Z
updated: 2026-09-13T13:16:11Z
---

## Description

Add `CommentsToDrafts` in `internal/youtrack/mapping/comments.go`: each YouTrack comment becomes a `core.Comment` draft with `author` from `login`, `author_name` from `fullName`, `author_email` from `email`, `created` and `updated` converted from unix milliseconds to RFC 3339, `kind: comment`, the body run through `NormalizeDescription`, and `external: [{system: "youtrack", id: <comment id>, url}]`. The file-name timestamp rule of ADR-012 is the caller's concern, not this function's.

## Acceptance Criteria

- [ ] Author, timestamps, body and `external` are mapped for every comment.
- [ ] Unix millisecond timestamps convert correctly, including a nil `updated`.
- [ ] Table-driven tests cover the comment fixture from the report; `go test -race ./internal/youtrack/...` passes.
