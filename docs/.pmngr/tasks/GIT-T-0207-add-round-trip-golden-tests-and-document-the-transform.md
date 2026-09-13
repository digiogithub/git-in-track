---
id: GIT-T-0207
type: task
title: Add round-trip golden tests and document the transform
status: in_progress
priority: medium
parent: GIT-US-0083
milestone: GIT-M-0011
author: mcp
labels: [core, docs, agent-ok]
estimate: 2
created: 2026-09-13T13:20:39Z
updated: 2026-09-13T14:59:58Z
started: 2026-09-13T14:59:58Z
---

## Description

Add a full round-trip golden test — page to article to page — asserting that a page with front matter, wikilinks, attachments and a feedback block returns unchanged except for what is deliberately normalised, with a `-update` flag to regenerate the goldens. Then document the transform rules, including the title rule and the feedback-block rule, in `docs/03-data-model.md` next to the KB page section, and add a `CHANGELOG.md` entry.

## Acceptance Criteria

- [x] A round-trip golden test exists and is stable; goldens regenerate only through the `-update` flag.
- [ ] The transform rules are documented in `docs/03-data-model.md`.
- [ ] `CHANGELOG.md` has an entry; `go test -race ./internal/youtrack/...` and `make lint` pass.

## Notes

**Partial: the tests landed, the two documentation edits have not.** The agent that implemented this story owned `internal/youtrack/mapping/` only and was explicitly told not to touch anything outside it, so `docs/03-data-model.md` and `CHANGELOG.md` are untouched and still need the edit.

Tests: `internal/youtrack/mapping/kbgolden_test.go`. `TestPageRoundTripGolden` records the whole page -> article -> page cycle in `testdata/kb-handbook.roundtrip.golden.txt`, and `TestPageRoundTripIsIdempotent` asserts the fixed point. The goldens are plain text with `=== section ===` headers rather than JSON, because a transform whose output is Markdown has to be reviewable as Markdown; they share the `-update` flag declared in `golden_test.go` and were read by hand, not regenerated blindly. `go test -race ./internal/youtrack/...` passes and `golangci-lint run ./internal/youtrack/...` reports zero issues.

The rules to copy into `docs/03-data-model.md` are written out in the package comment at the top of `internal/youtrack/mapping/kbpage.go`: the title lives only in `summary`, the `## Feedback` block never leaves the repository, front matter is stripped up and rebuilt down, a wikilink becomes an article link only for a published page, an attachment is addressed by file name, and the YouTrack `{color:…}` / `{width=…}` extensions are passed through up and preserved down.
