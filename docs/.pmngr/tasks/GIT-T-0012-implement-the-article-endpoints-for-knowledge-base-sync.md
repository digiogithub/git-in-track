---
id: GIT-T-0012
type: task
title: Implement the article endpoints for knowledge-base sync
status: done
priority: medium
parent: GIT-US-0046
milestone: GIT-M-0011
author: mcp
labels: [server, agent-ok]
estimate: 3
created: 2026-09-13T13:15:31Z
updated: 2026-09-13T14:07:53Z
started: 2026-09-13T14:07:40Z
closed: 2026-09-13T14:07:53Z
---

## Description

Add `Article(ctx, id)` over `GET /api/articles/{id}`, `CreateArticle(ctx, in)` over `POST /api/articles` (which requires a `project`), `UpdateArticle(ctx, id, in)` over `POST /api/articles/{id}` — YouTrack has no PUT, updates are a partial POST to the resource — and `ChildArticles(ctx, id)` over `GET /api/articles/{id}/childArticles` for walking the tree. The article `content` field is Markdown and is untrusted third-party content: return it as-is and document that callers must sanitise before rendering.

## Acceptance Criteria

- [x] The four methods are implemented with the documented field selectors and typed results.
- [x] An update sends only the fields the caller set, and a create without a project fails with a clear error before any request is made.
- [x] The package doc comment states that article and issue text is untrusted content.
- [x] `go test -race ./internal/youtrack/...` covers create, update and a child-article walk.

## Notes

`ArticleInput` uses pointer fields so an update sends exactly the keys the caller set; `ParentArticleID` pointing at the empty string sends an explicit null, which detaches the article from its parent. `ProjectID` is sent only on a create, because the project of an article is read-only afterwards. `ordinal` is deliberately not sent: it is documented read-only and the server ignores it.

`SearchArticles` / `SearchAllArticles` and `ArticleAttachments` were added alongside; article searches go through `EnsureOrderBy` too, unlike the reference client, whose article paging has no ordering clause and therefore loses rows.
