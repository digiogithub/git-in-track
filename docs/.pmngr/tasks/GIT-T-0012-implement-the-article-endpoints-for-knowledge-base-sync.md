---
id: GIT-T-0012
type: task
title: Implement the article endpoints for knowledge-base sync
status: todo
priority: medium
parent: GIT-US-0046
milestone: GIT-M-0011
author: mcp
labels: [server, agent-ok]
estimate: 3
created: 2026-09-13T13:15:31Z
updated: 2026-09-13T13:15:31Z
---

## Description

Add `Article(ctx, id)` over `GET /api/articles/{id}`, `CreateArticle(ctx, in)` over `POST /api/articles` (which requires a `project`), `UpdateArticle(ctx, id, in)` over `POST /api/articles/{id}` — YouTrack has no PUT, updates are a partial POST to the resource — and `ChildArticles(ctx, id)` over `GET /api/articles/{id}/childArticles` for walking the tree. The article `content` field is Markdown and is untrusted third-party content: return it as-is and document that callers must sanitise before rendering.

## Acceptance Criteria

- [ ] The four methods are implemented with the documented field selectors and typed results.
- [ ] An update sends only the fields the caller set, and a create without a project fails with a clear error before any request is made.
- [ ] The package doc comment states that article and issue text is untrusted content.
- [ ] `go test -race ./internal/youtrack/...` covers create, update and a child-article walk.
