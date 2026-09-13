---
id: GIT-T-0072
type: task
title: Download issue attachments into the attachments folder
status: todo
priority: medium
parent: GIT-US-0050
milestone: GIT-M-0011
author: mcp
labels: [server, agent-ok]
estimate: 3
created: 2026-09-13T13:17:05Z
updated: 2026-09-13T13:17:05Z
---

## Description

When `includeAttachments` is set, list attachments with `GET /api/issues/{id}/attachments` paged explicitly, resolve the signed relative `attachment.url` against the instance base URL — the signature is in the URL and the Bearer header is still sent, and redirects must be followed — and stream each file to `.pmngr/attachments/<ITEM-ID>/<name>.<pid>.<base36 ts>.part`, `stat` it and rename it into place so a partial download never looks complete, removing the temp file on error. Skip a file that already exists at the expected size. Record the resulting file names in the item's `attachments[]` front matter.

## Acceptance Criteria

- [ ] Attachments are listed with explicit paging and downloaded with the Bearer header and redirects followed.
- [ ] Downloads stream to a `.part` file and rename on success; the temp file is removed on error.
- [ ] An existing file of the expected size is skipped.
- [ ] `attachments[]` is set on the item; `go test -race ./internal/server/...` covers success, skip and failure with an httptest server.
