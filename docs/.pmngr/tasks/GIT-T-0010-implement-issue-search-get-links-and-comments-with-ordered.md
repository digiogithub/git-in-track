---
id: GIT-T-0010
type: task
title: Implement issue search, get, links and comments with ordered paging
status: done
priority: medium
parent: GIT-US-0046
milestone: GIT-M-0011
author: mcp
labels: [server, agent-ok]
estimate: 5
created: 2026-09-13T13:15:25Z
updated: 2026-09-13T14:06:58Z
started: 2026-09-13T14:06:43Z
closed: 2026-09-13T14:06:58Z
---

## Description

Add `SearchIssues(ctx, query, page)` over `GET /api/issues`, `Issue(ctx, id)` over `GET /api/issues/{id}`, `IssueLinks(ctx, id)` over `GET /api/issues/{id}/links`, `Comments(ctx, id, page)` over `GET /api/issues/{id}/comments` and `AddComment(ctx, id, text)` over `POST /api/issues/{id}/comments` with body `{"text": "…"}`. Add a paging helper that defaults `$top` to 100, pairs `$skip` with an explicit `order by:` appended to the query when the caller has not supplied one, and stops on a short page. Always send `$top` on the comments endpoint — the reference client omits it and truncates silently.

## Acceptance Criteria

- [x] Paging walks multiple pages, stops on a short page and never issues `$skip` without an `order by:`.
- [x] Issue, link and comment shapes decode into typed structs, including custom fields and `idReadable`.
- [x] `AddComment` posts the documented body and returns the created comment.
- [x] `go test -race ./internal/youtrack/...` covers a two-page walk and the missing-`order by:` guard.

## Notes

`IssueLinks` returns the result already filtered through `NonEmptyLinks`: YouTrack answers with one entry per (linkType, direction) pair including the empty ones, so an issue with a single link comes back among roughly twenty empty entries. The raw list is still visible on `Issue.Links` for callers that want it.

Custom-field values are kept as raw JSON in `CustomFieldValue` and decoded on demand with `Values()`, because the shape depends on the field kind; flattening them to display strings is the mapping layer's job, not this package's. The selector asks for `$type` on both the field and the value, which the reference client omits and which a typed writer needs.

Also added here: `SearchAllIssues`, `AllComments`, `Attachments`, `AttachmentURL` and `DownloadAttachment` (signed relative URL resolved against the base URL, bearer token still sent).
