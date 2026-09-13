---
id: GIT-T-0072
type: task
title: Download issue attachments into the attachments folder
status: done
priority: medium
parent: GIT-US-0050
milestone: GIT-M-0011
author: mcp
labels: [server, agent-ok]
estimate: 3
created: 2026-09-13T13:17:05Z
updated: 2026-09-13T15:59:01Z
started: 2026-09-13T15:58:43Z
closed: 2026-09-13T15:59:01Z
---

## Description

When `includeAttachments` is set, list attachments with `GET /api/issues/{id}/attachments` paged explicitly, resolve the signed relative `attachment.url` against the instance base URL — the signature is in the URL and the Bearer header is still sent, and redirects must be followed — and stream each file to `.pmngr/attachments/<ITEM-ID>/<name>.<pid>.<base36 ts>.part`, `stat` it and rename it into place so a partial download never looks complete, removing the temp file on error. Skip a file that already exists at the expected size. Record the resulting file names in the item's `attachments[]` front matter.

## Acceptance Criteria

- [x] Attachments are listed with explicit paging and downloaded with the Bearer header and redirects followed.
- [x] Downloads stream to a `.part` file and rename on success; the temp file is removed on error.
- [x] An existing file of the expected size is skipped.
- [x] `attachments[]` is set on the item; `go test -race ./internal/server/...` covers success, skip and failure with an httptest server.

## Notes

The listing and the signed-URL resolution were already in
`internal/youtrack`: `Client.Attachments` sends `$top` explicitly and
`Client.DownloadAttachment` resolves `attachment.url` against the base URL,
keeps the Bearer header and lets the injected HTTP client follow redirects. This
task is the file half, in `internal/server/youtrackimport.go`.

The rename happens only after the stream closed cleanly **and** the size on disk
matches what YouTrack reported, so a truncated transfer is refused rather than
installed; the temporary file is removed on every other path, cancellation
included. A remote file name is reduced to its base name before it is joined, so
a name carrying `../` cannot write outside the item's folder.

`attachments[]` is written by the vault's own import, which records the paths
and warns that the bytes are the job's problem — this is that job. The downloads
run **after** the batch has committed, because the item ids the files are filed
under only exist once the batch is written, and a failed download must leave the
imported item in place.
