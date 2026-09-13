---
id: GIT-T-0010
type: task
title: Implement issue search, get, links and comments with ordered paging
status: todo
priority: medium
parent: GIT-US-0046
milestone: GIT-M-0011
author: mcp
labels: [server, agent-ok]
estimate: 5
created: 2026-09-13T13:15:25Z
updated: 2026-09-13T13:15:25Z
---

## Description

Add `SearchIssues(ctx, query, page)` over `GET /api/issues`, `Issue(ctx, id)` over `GET /api/issues/{id}`, `IssueLinks(ctx, id)` over `GET /api/issues/{id}/links`, `Comments(ctx, id, page)` over `GET /api/issues/{id}/comments` and `AddComment(ctx, id, text)` over `POST /api/issues/{id}/comments` with body `{"text": "…"}`. Add a paging helper that defaults `$top` to 100, pairs `$skip` with an explicit `order by:` appended to the query when the caller has not supplied one, and stops on a short page. Always send `$top` on the comments endpoint — the reference client omits it and truncates silently.

## Acceptance Criteria

- [ ] Paging walks multiple pages, stops on a short page and never issues `$skip` without an `order by:`.
- [ ] Issue, link and comment shapes decode into typed structs, including custom fields and `idReadable`.
- [ ] `AddComment` posts the documented body and returns the created comment.
- [ ] `go test -race ./internal/youtrack/...` covers a two-page walk and the missing-`order by:` guard.
