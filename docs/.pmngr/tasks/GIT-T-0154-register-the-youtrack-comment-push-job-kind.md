---
id: GIT-T-0154
type: task
title: Register the youtrack.comment.push job kind
status: done
priority: medium
parent: GIT-US-0068
milestone: GIT-M-0011
author: mcp
labels: [server, agent-ok]
estimate: 3
created: 2026-09-13T13:19:03Z
updated: 2026-09-13T16:00:07Z
started: 2026-09-13T15:59:49Z
closed: 2026-09-13T16:00:07Z
---

## Description

Add `internal/server/youtrack_comments.go` registering a `youtrack.comment.push` `Kind` and `Handler` with the sync engine. The handler reads the comment, resolves the parent item's YouTrack `external` id, renders the body plus the attribution line, and posts `POST /api/issues/{id}/comments` with `{text}` — or, when the comment already carries a YouTrack `external` id, edits with `POST /api/issues/{id}/comments/{cid}`. An item with no YouTrack reference fails the job with a non-retryable error so the backoff loop does not spin.

## Acceptance Criteria

- [x] The job kind is registered and handles both the create and the edit path.
- [x] A missing item reference fails the job as non-retryable with a clear message.
- [x] Rate limiting and retry come from the engine, not from this handler.
- [ ] `go test -race ./internal/server/...` covers create, edit and the missing-link failure with a fake client.

## Notes

Landed as `internal/server/youtrackcomments.go`. The payload is the one
`internal/vault`'s `YouTrackCommentPush` enqueues — `{project, itemId,
commentPath}` — so the vault decides a push is needed and this runs it; nothing
pushes inline.

**The edit path is written and tested but cannot reach a real instance yet**,
which is why the test criterion is left unticked: the shipped `*youtrack.Client`
has `AddComment` and no `UpdateComment`, and `internal/youtrack` belongs to
another agent this wave. The handler therefore probes an optional
`youtrackCommentEditor` interface: a client that grows
`UpdateComment(ctx, issueID, commentID, text)` satisfies it with no change here,
and a client that has not fails the second push **terminally**, saying so,
rather than posting a second copy of the comment. The create path, the
missing-link failure and both branches of the edit decision are covered against
a fake in `youtrackcomments_test.go`.

Rate limiting and retry are the engine's and the client's: this handler contains
neither a sleep nor a loop.
