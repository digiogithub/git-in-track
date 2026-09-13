---
id: GIT-T-0210
type: task
title: Register the KB pull job kind
status: todo
priority: medium
parent: GIT-US-0087
milestone: GIT-M-0011
author: mcp
labels: [server, agent-ok]
estimate: 3
created: 2026-09-13T13:20:53Z
updated: 2026-09-13T13:20:53Z
---

## Description

Register a `youtrack.kb.pull` `Kind` and `Handler` that reads the article, and its descendants through `GET /api/articles/{id}/childArticles` when recursive, transforms it back with `ArticleToPage` and writes through `kb.write` so `WritePage` and its feedback pruning apply normally. Every listing that pages must send an explicit ordering clause — the reference CLI's article listing omits one and is a latent paging bug.

## Acceptance Criteria

- [ ] A single page and a recursive subtree both pull and write through `kb.write`.
- [ ] Article listings that page always send an ordering clause.
- [ ] The local feedback block survives the pull.
- [ ] `go test -race ./internal/server/...` covers single and recursive pull with a fake client.
