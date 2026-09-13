---
id: GIT-T-0154
type: task
title: Register the youtrack.comment.push job kind
status: todo
priority: medium
parent: GIT-US-0068
milestone: GIT-M-0011
author: mcp
labels: [server, agent-ok]
estimate: 3
created: 2026-09-13T13:19:03Z
updated: 2026-09-13T13:19:03Z
---

## Description

Add `internal/server/youtrack_comments.go` registering a `youtrack.comment.push` `Kind` and `Handler` with the sync engine. The handler reads the comment, resolves the parent item's YouTrack `external` id, renders the body plus the attribution line, and posts `POST /api/issues/{id}/comments` with `{text}` — or, when the comment already carries a YouTrack `external` id, edits with `POST /api/issues/{id}/comments/{cid}`. An item with no YouTrack reference fails the job with a non-retryable error so the backoff loop does not spin.

## Acceptance Criteria

- [ ] The job kind is registered and handles both the create and the edit path.
- [ ] A missing item reference fails the job as non-retryable with a clear message.
- [ ] Rate limiting and retry come from the engine, not from this handler.
- [ ] `go test -race ./internal/server/...` covers create, edit and the missing-link failure with a fake client.
