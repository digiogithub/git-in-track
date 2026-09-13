---
id: GIT-T-0207
type: task
title: Add round-trip golden tests and document the transform
status: done
priority: medium
parent: GIT-US-0083
milestone: GIT-M-0011
author: mcp
labels: [core, docs, agent-ok]
estimate: 2
created: 2026-09-13T13:20:39Z
updated: 2026-09-13T16:19:53Z
started: 2026-09-13T14:59:58Z
closed: 2026-09-13T16:19:53Z
---

## Description

Add a full round-trip golden test — page to article to page — asserting that a page with front matter, wikilinks, attachments and a feedback block returns unchanged except for what is deliberately normalised, with a `-update` flag to regenerate the goldens. Then document the transform rules, including the title rule and the feedback-block rule, in `docs/03-data-model.md` next to the KB page section, and add a `CHANGELOG.md` entry.

## Acceptance Criteria

- [x] A round-trip golden test exists and is stable; goldens regenerate only through the `-update` flag.
- [x] The transform rules are documented in `docs/03-data-model.md`.
- [ ] `CHANGELOG.md` has an entry; `go test -race ./internal/youtrack/...` and `make lint` pass.

## Notes

Tests: `internal/youtrack/mapping/kbgolden_test.go`. `TestPageRoundTripGolden` records the whole page -> article -> page cycle in `testdata/kb-handbook.roundtrip.golden.txt`, and `TestPageRoundTripIsIdempotent` asserts the fixed point. The goldens are plain text with `=== section ===` headers rather than JSON, because a transform whose output is Markdown has to be reviewable as Markdown; they share the `-update` flag declared in `golden_test.go`.

**Documentation landed** in `docs/03-data-model.md` §14.5 "Publishing a page as a YouTrack article",
placed immediately after §14.4 (the feedback block it depends on). The five rules are written as
R-KB-1 … R-KB-5 — the title living only in `summary`, the `## Feedback` block never leaving the
repository, front matter stripped up and rebuilt down, a wikilink degrading to text unless its
target is published, an attachment addressed by file name — plus the paragraph explaining why
`{color:…}` and `{width=…}` are passed through here and stripped for an issue description (R-YT-6):
an issue description is imported once, a page is round-tripped.

One claim was softened against the code: a page with neither a title nor a leading H1 is **not**
refused, it produces an empty summary and a warning saying YouTrack will not create an article
without one.

`CHANGELOG.md` was not touched — another agent owns it this wave — so that criterion is unticked.
`make lint` does not lint Markdown, and its current failures are in other agents' in-flight files.
