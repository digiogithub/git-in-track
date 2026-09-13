---
id: GIT-T-0035
type: task
title: Map YouTrack comments to comment drafts
status: done
priority: medium
parent: GIT-US-0045
milestone: GIT-M-0011
author: mcp
labels: [core, agent-ok]
estimate: 1
created: 2026-09-13T13:16:11Z
updated: 2026-09-13T14:35:03Z
started: 2026-09-13T14:34:36Z
closed: 2026-09-13T14:35:03Z
---

## Description

Add `CommentsToDrafts` in `internal/youtrack/mapping/comments.go`: each YouTrack comment becomes a `core.Comment` draft with `author` from `login`, `author_name` from `fullName`, `author_email` from `email`, `created` and `updated` converted from unix milliseconds to RFC 3339, `kind: comment`, the body run through `NormalizeDescription`, and `external: [{system: "youtrack", id: <comment id>, url}]`. The file-name timestamp rule of ADR-012 is the caller's concern, not this function's.

## Acceptance Criteria

- [x] Author, timestamps, body and `external` are mapped for every comment.
- [x] Unix millisecond timestamps convert correctly, including a nil `updated`.
- [x] Table-driven tests cover the comment fixture from the report; `go test -race ./internal/youtrack/...` passes.

## Notes

Landed as `comments.go`. **`core.CommentDraft` has no `external` and no `updated` field** — `core.Comment` has both, but the draft that creates it does not — so the function returns a `mapping.CommentDraft` wrapper `{Draft core.CommentDraft; External core.External; Updated core.Timestamp}` and the importer records the two extras. If a later story adds `External` to `core.CommentDraft`, this wrapper collapses; it is not this package's file to change.

Signature is `CommentsToDrafts(comments []youtrack.Comment, issueID string, opts Options)` — the issue id is needed because a comment's URL is the issue's with a `#focus=Comments-<id>` fragment. Deleted comments are skipped; an empty body is skipped with a warning, since `core.Store.AddComment` refuses one; drafts come back in created order.
