---
id: GIT-T-0208
type: task
title: Register the KB publish job kind for a single page
status: done
priority: medium
parent: GIT-US-0087
milestone: GIT-M-0011
author: mcp
labels: [server, agent-ok]
estimate: 3
created: 2026-09-13T13:20:44Z
updated: 2026-09-13T16:01:29Z
started: 2026-09-13T16:01:03Z
closed: 2026-09-13T16:01:29Z
---

## Description

Add `internal/server/youtrack_kb.go` registering a `youtrack.kb.publish` `Kind` and `Handler`. For a page with no YouTrack `external` reference it creates the article with `POST /api/articles` including `project` in the body — `project` is read-only afterwards and can only be set at creation — and for a linked page it updates with `POST /api/articles/{id}`. On success it writes the article id and url into the page front matter through `kb.write` (`internal/vault/vault.go:1356`), quoting the page rev.

## Acceptance Criteria

- [x] An unlinked page is created with `project` in the creation body; a linked page is updated.
- [x] The article id and url are written back through `kb.write` rev-guarded.
- [x] A stale rev on the write-back is reported, never forced.
- [x] `go test -race ./internal/server/...` covers create, update and the stale-rev path with a fake client.

## Notes

Landed as `internal/server/youtrackkb.go`, shared with the pull of GIT-T-0210.

The reference written back carries more than the id and the url: it also records
`key`, the fingerprint of the content that was published, and `synced_at`. That
pair is what makes the next run able to tell a local edit from a remote one
without guessing from timestamps (GIT-T-0211), so writing it is part of the
publish rather than an extra.

Two things the mapping layer cannot do are done here and reported rather than
faked: a page with neither a title nor a leading heading fails terminally,
because an article cannot be created without a summary; and a page referencing
local files logs that this build cannot upload them, since
`ArticlePayload.Attachments` lists what would need uploading and
`internal/youtrack` has no article-attachment upload yet. The content already
refers to them by name, so the article is published and the gap is visible
instead of producing silently broken images.
