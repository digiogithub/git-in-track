---
id: GIT-T-0002
type: task
title: Read and write `external` in the front-matter parser and serializer
status: done
priority: medium
parent: GIT-US-0044
milestone: GIT-M-0011
author: mcp
labels: [core, agent-ok]
estimate: 3
created: 2026-09-13T13:14:45Z
updated: 2026-09-13T14:00:32Z
started: 2026-09-13T14:00:26Z
closed: 2026-09-13T14:00:32Z
---

## Description

Wire `external` through `ParseItem` (`internal/core/frontmatter.go:189`), `ParseComment` (`:289`), `SerializeItem` (`:362`) and `SerializeComment` (`:404`), plus the KB page reader and writer in `internal/core/kb.go`. Place the key at its documented position in the canonical key order so serialization is deterministic, and add golden fixtures under `internal/core/testdata/` covering zero, one and several entries and an entry with only `system` and `id`.

## Acceptance Criteria

- [x] Parse→serialize is byte-identical for every new golden fixture, and existing goldens are unchanged.
- [x] The key order matches what `docs/03-data-model.md` §3.2 will state.
- [x] `go test -race ./internal/core/...` passes; goldens were reviewed by hand, not regenerated blindly.

## Notes

Canonical position: `external` sits between `reactions` and `attachments`, so an item reads
`links, external, attachments, custom, inbox, deleted` and a comment reads
`reactions, external, attachments`. `TestExternalCanonicalKeyOrder` pins the full order.
Entries are emitted as one flow mapping per line, like `links`, so a change to one reference is a
one-line diff. The parser lower-cases the system, trims the id and drops a duplicate `(system, id)`
pair, which the golden fixture `testdata/external-story.md` exercises. KB pages have no serializer
(`WritePage` writes caller bytes), so only the reader was needed there.
