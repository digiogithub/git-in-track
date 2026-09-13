---
id: GIT-T-0202
type: task
title: Upload local attachments and rewrite their references
status: todo
priority: medium
parent: GIT-US-0083
milestone: GIT-M-0011
author: mcp
labels: [server, agent-ok]
estimate: 3
created: 2026-09-13T13:20:27Z
updated: 2026-09-13T13:20:27Z
---

## Description

Add the attachment step used when publishing: collect the local files a page's body references, upload them with `POST /api/articles/{id}/attachments` as `multipart/form-data` — deliberately without a JSON content type — and rewrite the body refs to the filenames YouTrack resolves against the article's own attachments. Skip a file already present on the article with the same name and size.

## Acceptance Criteria

- [ ] Referenced local files are uploaded as multipart with no JSON content type set.
- [ ] Body refs are rewritten to the article attachment filenames.
- [ ] An attachment already present with the same name and size is skipped.
- [ ] `go test -race ./internal/server/...` covers upload, skip and failure against an httptest server.
