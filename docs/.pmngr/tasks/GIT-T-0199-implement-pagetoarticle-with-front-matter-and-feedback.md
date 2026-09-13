---
id: GIT-T-0199
type: task
title: Implement PageToArticle with front matter and feedback stripping
status: done
priority: medium
parent: GIT-US-0083
milestone: GIT-M-0011
author: mcp
labels: [core, agent-ok]
estimate: 3
created: 2026-09-13T13:20:18Z
updated: 2026-09-13T14:58:54Z
started: 2026-09-13T14:58:41Z
closed: 2026-09-13T14:58:54Z
---

## Description

Add `internal/youtrack/mapping/article.go` with `PageToArticle(page core.KBPage) (ArticlePayload, []Warning)`. It strips the YAML front matter, removes the block delimited by `<!-- gintrack:feedback:begin -->` and `<!-- gintrack:feedback:end -->` using the parser in `internal/core/kbfeedback.go:179`, and puts the title in `summary` only — never also as an H1 in `content`. Remember that the article body field is `content`, not `description`.

## Acceptance Criteria

- [x] Front matter and the feedback block never appear in the payload.
- [x] The title is emitted in `summary` and the body carries no duplicate H1.
- [x] The payload uses `content` for the body.
- [x] Golden tests cover a page with and without a feedback block; `go test -race ./internal/youtrack/...` passes.

## Notes

Landed as `internal/youtrack/mapping/kbpage.go` (the file is named after the page, not the article, because it holds both directions). `PageToArticle(page core.KBPage, opts PageOptions) (ArticlePayload, []Warning)`; `ArticlePayload` carries `Summary`, `Content` and `Attachments`, and `ArticlePayload.Input()` renders it as a `youtrack.ArticleInput` whose `Content` pointer is the `content` field.

The feedback block is split off by `splitFeedback`, which mirrors `core.parseKbFeedbackDoc`'s recognition rule (end marker must be the last non-blank line, begin marker outside a code fence) rather than calling into core: core owns writing the block, this package only ever reads it. Goldens: `testdata/kb-handbook.up.golden.txt` (with a block) and `testdata/kb-notes.up.golden.txt` (without).
