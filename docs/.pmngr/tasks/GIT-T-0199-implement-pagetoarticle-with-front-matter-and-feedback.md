---
id: GIT-T-0199
type: task
title: Implement PageToArticle with front matter and feedback stripping
status: todo
priority: medium
parent: GIT-US-0083
milestone: GIT-M-0011
author: mcp
labels: [core, agent-ok]
estimate: 3
created: 2026-09-13T13:20:18Z
updated: 2026-09-13T13:20:18Z
---

## Description

Add `internal/youtrack/mapping/article.go` with `PageToArticle(page core.KBPage) (ArticlePayload, []Warning)`. It strips the YAML front matter, removes the block delimited by `<!-- gintrack:feedback:begin -->` and `<!-- gintrack:feedback:end -->` using the parser in `internal/core/kbfeedback.go:179`, and puts the title in `summary` only — never also as an H1 in `content`. Remember that the article body field is `content`, not `description`.

## Acceptance Criteria

- [ ] Front matter and the feedback block never appear in the payload.
- [ ] The title is emitted in `summary` and the body carries no duplicate H1.
- [ ] The payload uses `content` for the body.
- [ ] Golden tests cover a page with and without a feedback block; `go test -race ./internal/youtrack/...` passes.
