---
id: GIT-T-0202
type: task
title: Upload local attachments and rewrite their references
status: done
priority: medium
parent: GIT-US-0083
milestone: GIT-M-0011
author: mcp
labels: [server, agent-ok]
estimate: 3
created: 2026-09-13T13:20:27Z
updated: 2026-09-13T15:30:22Z
started: 2026-09-13T14:59:46Z
closed: 2026-09-13T15:30:22Z
---

## Description

Add the attachment step used when publishing: collect the local files a page's body references, upload them with `POST /api/articles/{id}/attachments` as `multipart/form-data` — deliberately without a JSON content type — and rewrite the body refs to the filenames YouTrack resolves against the article's own attachments. Skip a file already present on the article with the same name and size.

## Acceptance Criteria

- [x] Referenced local files are uploaded as multipart with no JSON content type set.
- [x] Body refs are rewritten to the article attachment filenames.
- [x] An attachment already present with the same name and size is skipped.
- [x] `go test -race ./internal/youtrack/...` covers upload, skip and failure against an httptest server.

## Notes

**Partial: the pure half landed, the transport half has not.** This task spans two packages. The rewriting and the collection of what to upload are pure and live in `internal/youtrack/mapping/kblinks.go`; the HTTP upload and the skip-if-present check belong to `internal/server` (or to `internal/youtrack` as a client method), which the agent that did this work did not own.

What exists now: `PageToArticle` returns `ArticlePayload.Attachments`, a `[]AttachmentRef{Name, Path}` ordered by name, where `Name` is the bare file name YouTrack will resolve the reference by and `Path` is the vault-relative path of the bytes to upload. `Content` already refers to the files by `Name`, so the upload and the article update can be sent in either order. Rules applied: a URL, an anchor and a link to another `.md` page are left alone; a reference climbing out of the vault is refused with a warning and never uploaded; two different files sharing a base name cannot both be resolved by that name, so the second is reported and left as it was rather than silently pointed at the first one's bytes. `ArticleToPage` inverts it using the paths the existing page referenced.

### What landed for the transport half

`internal/youtrack/attachments.go`, covered by `internal/youtrack/attachments_upload_test.go` against `httptest` — the tests live in the client package rather than in `internal/server`, because that is where the call landed:

```go
func (c *Client) UploadArticleAttachment(ctx, articleID, name string, content io.Reader) (Attachment, error)
func (c *Client) UploadIssueAttachment(ctx, issueID, name string, content io.Reader) (Attachment, error)
func (c *Client) SyncArticleAttachments(ctx, articleID string, fsys fs.FS, files []FileRef) (UploadResult, error)
func (c *Client) SyncIssueAttachments(ctx, issueID string, fsys fs.FS, files []FileRef) (UploadResult, error)

type FileRef struct{ Name, Path string }        // same shape as mapping.AttachmentRef
type UploadResult struct{ Uploaded []Attachment; Skipped []string }
```

The upload builds its request outside the JSON path, so `Content-Type` is `multipart/form-data` with its own boundary and never `application/json`; the body is buffered because a retried attempt has to send the same bytes again, capped by `MaxAttachmentSize` (64 MiB). `Sync*` lists the current attachments once and skips every name already attached with the same size, including a second reference to a file it just uploaded. Paths are `fs.FS` paths, validated with `fs.ValidPath`, so a reference climbing out of the vault is refused before any request. The issue variant came free and is there for the push-back story.
