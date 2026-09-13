---
id: GIT-US-0083
type: story
title: KB page and YouTrack article content transform
status: in_review
priority: medium
parent: GIT-EP-0014
milestone: GIT-M-0011
author: mcp
labels: [core, server]
estimate: 8
created: 2026-09-13T13:14:26Z
updated: 2026-09-13T15:00:49Z
started: 2026-09-13T15:00:28Z
---

## Description

As a writer publishing documentation, I want the transform between a KB page and a YouTrack article to be one explicit, tested function pair, so that a round trip does not duplicate titles, leak internal feedback or break links.

Add the transform to `internal/youtrack/mapping` (alongside the issue mapping from GIT-EP-0012): `PageToArticle(page core.KBPage) (ArticlePayload, []Warning)` and `ArticleToPage(article, existing core.KBPage) (body string, front map[string]any, []Warning)`. Going up, the YAML front matter is stripped, the `## Feedback` block delimited by `<!-- gintrack:feedback:begin -->` and `<!-- gintrack:feedback:end -->` is removed entirely, the title lives in `summary` and never also as an H1 in `content`, and `[[wikilinks]]` are rewritten to article links using the `external` references of the target pages (an unresolvable target degrades to plain text with a warning rather than a dead link). Local attachments referenced by the body are uploaded and the refs rewritten to the filenames YouTrack resolves against the article's own attachments.

Coming down, `content` becomes the page body with the `summary` re-applied according to the same single rule, article links are turned back into wikilinks where they point at pages we know, and the page's existing feedback block is preserved untouched — remembering that `WritePage` prunes it on every write (ADR-030 R-FB-4, `internal/core/kbfeedback.go:166`), so the pruning must never be read as a remote change. YouTrack-specific Markdown (`{color:red}…{color}`, `{width=300px}`) is passed through on the way up and stripped or preserved explicitly on the way down; bare issue ids are auto-linked by YouTrack and must not be wrapped.

## Acceptance Criteria

- [x] `PageToArticle` strips front matter and the feedback block and never emits the title both as `summary` and as an H1.
- [x] Wikilinks resolve to article links via the targets' `external` references; unresolvable targets degrade to text with a warning.
- [ ] Local attachments are uploaded and their refs rewritten to the article's attachment filenames.
- [x] `ArticleToPage` restores the title by the same single rule, converts known article links back to wikilinks and preserves the local feedback block.
- [x] The feedback block never appears in an outgoing payload and its local pruning is never reported as a remote change.
- [x] Bare issue ids and YouTrack-specific markup are handled explicitly and documented.
- [x] Golden tests under `internal/youtrack/mapping/testdata/` cover both directions and a full round trip; `go test -race ./internal/youtrack/...` passes and goldens are reviewed, not blindly regenerated.

## Notes

Depends on GIT-EP-0011 for the `external` field on KB pages and for the client. `core.KBPage.FrontMatter` is already a free `map[string]any` (`internal/core/kb.go:20-45`), so no core change is needed for the reference itself.

Article field names differ from issues: an article's body is `content`, an issue's is `description` — a classic bug source when one adapter serves both. Attachment upload is `POST /api/articles/{id}/attachments` as `multipart/form-data` with no JSON content type. Feedback block rules and helpers: `internal/core/kbfeedback.go` (`AddKbFeedback` :113, `PruneKbFeedback` :166, `ParseKbFeedback` :179).

Do NOT push the `## Feedback` block upstream under any option. Do NOT reuse the reference CLI's hand-rolled YAML writer; use `gopkg.in/yaml.v3`.

### What landed

`internal/youtrack/mapping/kbpage.go` and `kblinks.go`, pure as the rest of the package: no HTTP, no filesystem, no clock. The five rules of the transform are written out in the package comment at the top of `kbpage.go`.

Exported API:

```go
func PageToArticle(page core.KBPage, opts PageOptions) (ArticlePayload, []Warning)
func ArticleToPage(article youtrack.Article, existing core.KBPage, opts PageOptions) (body string, front map[string]any, warnings []Warning)
func EqualContent(a, b string) bool

type PageOptions struct { Options; Pages *PageIndex }
type ArticlePayload struct { Summary, Content string; Attachments []AttachmentRef }
func (ArticlePayload) Input() youtrack.ArticleInput
type AttachmentRef struct { Name, Path string }
type PageRef struct { Slug, Title, ArticleID, URL string }
func NewPageIndex(refs []PageRef) *PageIndex
func (*PageIndex) BySlug/ByArticle/ByURL(string) (PageRef, bool)
```

### Two criteria left open, deliberately

- **Attachment upload** (GIT-T-0202). The pure half is done — `ArticlePayload.Attachments` names exactly which local file to upload under which name, and `Content` already refers to it by that name — but the multipart `POST /api/articles/{id}/attachments` and the skip-if-present check live in a package this work did not own. See the task for what is left.
- **`docs/03-data-model.md` and `CHANGELOG.md`** (GIT-T-0207). The round-trip goldens landed; the two documentation edits are outside the owned path and are still open. The text to copy is the package comment at the top of `kbpage.go`.
