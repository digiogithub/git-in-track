---
id: GIT-T-0204
type: task
title: Implement ArticleToPage preserving the local feedback block
status: todo
priority: medium
parent: GIT-US-0083
milestone: GIT-M-0011
author: mcp
labels: [core, agent-ok]
estimate: 3
created: 2026-09-13T13:20:31Z
updated: 2026-09-13T13:20:31Z
---

## Description

Implement `ArticleToPage(article, existing core.KBPage)`: the article's `content` becomes the page body with the title re-applied by the same single rule used upward, article links convert back to wikilinks, and the existing page's feedback block is carried over untouched. Because `WritePage` prunes the feedback block on every write (ADR-030 R-FB-4, `internal/core/kbfeedback.go:166`), the transform must never interpret that pruning as a remote change.

## Acceptance Criteria

- [ ] The page body and title are restored by the same rule used on the way up.
- [ ] The existing feedback block is preserved verbatim.
- [ ] Feedback-block pruning is never treated as a change to the remote content.
- [ ] Golden tests cover a pull onto a page with feedback; `go test -race ./internal/youtrack/...` passes.
