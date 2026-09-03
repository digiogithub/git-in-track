---
id: GIT-US-0002
type: story
title: Parse Markdown front matter into the core model
status: backlog
created: 2026-09-03T00:00:00Z
updated: 2026-09-03T00:00:00Z
author: team
priority: critical
parent: GIT-EP-0001
milestone: GIT-M-0001
estimate: 5
labels: [core]
links:
  - kind: blocked_by
    target: GIT-US-0001
---

## Description

As the shared core, I need to read a Markdown file with YAML front matter into a typed
`Item`, and write it back out, so that every other part of the product works on a model
instead of on text.

The parser handles the common fields from the data model (`id`, `type`, `title`, `status`,
`created`, `updated`, `author`, `assignees`, `labels`, `priority`, `parent`, `milestone`,
`estimate`, `effort`, `due`, `links`) and preserves any unknown field verbatim, so a file
written by a newer version is never silently damaged by an older one. `rev` is never
stored: it is computed as a content hash on load.

Serialisation is canonical — fixed key order, one entry per line, no flow collections — so
that concurrent edits produce clean, mergeable diffs (risk R5). Round-tripping a file that
is already canonical must be byte-identical.

## Acceptance Criteria

- [ ] `ParseItem` reads all fixture files in `testdata/fixtures/project-basic`.
- [ ] Unknown front-matter keys survive a parse/serialise round trip.
- [ ] Serialisation is canonical and deterministic across runs and platforms.
- [ ] A canonical file round-trips byte-for-byte, proven by golden tests with `-update`.
- [ ] Missing or malformed front matter returns `ErrInvalidFrontMatter` with file and line.
- [ ] `rev` is computed from content and is not written to the file.
- [ ] CRLF and LF inputs both parse; the original line ending is preserved on write.
- [ ] `FuzzParseFrontMatter` runs clean for the CI budget without panicking.
- [ ] Package coverage is at or above 85 %.
