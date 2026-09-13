---
id: GIT-T-0200
type: task
title: Rewrite wikilinks to article links and back
status: done
priority: medium
parent: GIT-US-0083
milestone: GIT-M-0011
author: mcp
labels: [core, agent-ok]
estimate: 3
created: 2026-09-13T13:20:22Z
updated: 2026-09-13T14:59:11Z
started: 2026-09-13T14:58:57Z
closed: 2026-09-13T14:59:11Z
---

## Description

Add the link rewriter: going up, resolve each `[[wikilink]]` to the target page's YouTrack article reference (using `core.ParseWikilink`, `internal/core/kb.go:206`) and emit an article link; an unresolvable target degrades to plain text with a warning rather than a dead link. Coming down, article links pointing at pages we know are converted back to wikilinks, and everything else is left as is.

## Acceptance Criteria

- [x] Resolvable wikilinks become article links; unresolvable ones degrade to text with a warning.
- [x] Known article links convert back to wikilinks and unknown ones are left untouched.
- [x] A round trip of a page with both kinds of link is stable.
- [x] Golden tests cover all four cases; `go test -race ./internal/youtrack/...` passes.

## Notes

Landed as `internal/youtrack/mapping/kblinks.go`. Resolution goes through `PageIndex` (`NewPageIndex([]PageRef)`), which the caller builds from its own index — this package resolves nothing on its own, the same seam `Relations` draws on the issue side. A `PageRef` with an empty `ArticleID` is a page that exists but was never published, and a link to it degrades to text just like an unknown target.

Two decisions worth a reviewer's attention:

- The link text of a wikilink without an alias is the **target as written**, not the page title. That is what makes the downward conversion the exact inverse: a title would come back as an alias the author never typed. `[[a/b]] -> [a/b](url) -> [[a/b]]`, `[[a/b|x]] -> [x](url) -> [[a/b|x]]`.
- "Stable" is idempotence, not identity. A wikilink that resolves to nothing is flattened to text on purpose and cannot come back, so `TestPageRoundTripIsIdempotent` asserts the transform reaches a fixed point after one pass. Bare issue ids are never wrapped in either direction (YouTrack auto-links them), and code fences and code spans are exempt from every rule.
