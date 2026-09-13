---
id: GIT-T-0204
type: task
title: Implement ArticleToPage preserving the local feedback block
status: done
priority: medium
parent: GIT-US-0083
milestone: GIT-M-0011
author: mcp
labels: [core, agent-ok]
estimate: 3
created: 2026-09-13T13:20:31Z
updated: 2026-09-13T14:59:28Z
started: 2026-09-13T14:59:15Z
closed: 2026-09-13T14:59:28Z
---

## Description

Implement `ArticleToPage(article, existing core.KBPage)`: the article's `content` becomes the page body with the title re-applied by the same single rule used upward, article links convert back to wikilinks, and the existing page's feedback block is carried over untouched. Because `WritePage` prunes the feedback block on every write (ADR-030 R-FB-4, `internal/core/kbfeedback.go:166`), the transform must never interpret that pruning as a remote change.

## Acceptance Criteria

- [x] The page body and title are restored by the same rule used on the way up.
- [x] The existing feedback block is preserved verbatim.
- [x] Feedback-block pruning is never treated as a change to the remote content.
- [x] Golden tests cover a pull onto a page with feedback; `go test -race ./internal/youtrack/...` passes.

## Notes

`ArticleToPage(article youtrack.Article, existing core.KBPage, opts PageOptions) (body string, front map[string]any, warnings []Warning)`.

The single title rule, applied here in reverse: the summary is written to the `title` key of the returned front matter and **nothing is prepended to the body**. A leading H1 in the article content is cut if one is there, so an article edited by hand in YouTrack cannot reintroduce the duplicate. The feedback block is taken verbatim from `existing.Body` and re-appended after the new content; the front matter is `existing.FrontMatter` key for key, with `title` refreshed and the `external` entry for `youtrack` replaced in place (other systems' entries are kept, in their original order).

The pruning rule is `EqualContent(a, b string) bool`, which strips front matter and the feedback block from both sides before comparing. **The publish and pull jobs must use it instead of comparing bodies**, otherwise every locally added, moved or pruned note reads as a remote change.
