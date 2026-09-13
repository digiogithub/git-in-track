---
id: GIT-T-0225
type: task
title: Add UpdateComment to the YouTrack client so a pushed comment can be edited
status: done
priority: medium
parent: GIT-US-0068
milestone: GIT-M-0011
author: mcp
labels: [core, agent-ok]
estimate: 2
created: 2026-09-13T16:20:42Z
updated: 2026-09-13T16:20:52Z
started: 2026-09-13T16:20:45Z
closed: 2026-09-13T16:20:52Z
---

## Description

The `youtrack.comment.push` handler needs `UpdateComment(ctx, issueID, commentID, text)` to make a re-delivered job edit the comment it already posted instead of posting a second copy. The client did not have it, so the handler probed for an optional interface and failed the job terminally rather than risk duplicating a comment — the honest answer, but a dead end.

Add the edit call to `internal/youtrack`.

## Acceptance Criteria

- [x] `UpdateComment` posts to `/api/issues/{id}/comments/{commentID}` with `{"text": ...}` and returns the comment as the server stored it.
- [x] Empty issue id, comment id or text are refused locally with `ErrInvalidInput`, before any request is made.
- [x] The token is redacted on every error path, and the error is an `APIError` naming the comment path.
- [x] `httptest` coverage with a golden fixture under `testdata/`; no wall-clock sleeps and no network.

## Notes

Landed in `internal/youtrack/issues.go` next to `AddComment`, with tests in
`issues_test.go` and the fixture `testdata/comment_updated.json`.

**The verb is POST, not PUT.** YouTrack addresses an existing comment with the
same method it creates one with, the comment id in the path being the whole of
the difference — which is why this package has no `put` helper and none was
added. The retry test asserts exactly that property: a retried attempt still
carries the comment id, so a retry is an edit and never a second comment.

Empty text is refused rather than sent, because it would clear the comment
rather than edit it; deleting a remote comment is a deliberate act, not the
result of an empty string.
