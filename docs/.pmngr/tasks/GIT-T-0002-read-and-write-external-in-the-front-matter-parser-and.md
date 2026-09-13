---
id: GIT-T-0002
type: task
title: Read and write `external` in the front-matter parser and serializer
status: todo
priority: medium
parent: GIT-US-0044
milestone: GIT-M-0011
author: mcp
labels: [core, agent-ok]
estimate: 3
created: 2026-09-13T13:14:45Z
updated: 2026-09-13T13:14:45Z
---

## Description

Wire `external` through `ParseItem` (`internal/core/frontmatter.go:189`), `ParseComment` (`:289`), `SerializeItem` (`:362`) and `SerializeComment` (`:404`), plus the KB page reader and writer in `internal/core/kb.go`. Place the key at its documented position in the canonical key order so serialization is deterministic, and add golden fixtures under `internal/core/testdata/` covering zero, one and several entries and an entry with only `system` and `id`.

## Acceptance Criteria

- [ ] Parse→serialize is byte-identical for every new golden fixture, and existing goldens are unchanged.
- [ ] The key order matches what `docs/03-data-model.md` §3.2 will state.
- [ ] `go test -race ./internal/core/...` passes; goldens were reviewed by hand, not regenerated blindly.
