---
id: GIT-T-0147
type: task
title: Add the external field to core.Comment
status: todo
priority: medium
parent: GIT-US-0068
milestone: GIT-M-0011
author: mcp
labels: [core, agent-ok]
estimate: 3
created: 2026-09-13T13:18:54Z
updated: 2026-09-13T13:18:54Z
---

## Description

Add the `External` field to `core.Comment` (`internal/core/model.go:377-400`) with the same `[{system, id, url, key?, synced_at?}]` shape agreed for items, and teach `ParseComment` (`internal/core/frontmatter.go:289`) and `SerializeComment` (`:404`) to read and emit it in the canonical key order. Unknown keys must keep round-tripping through `Comment.Extra`. The code must stay WASM-safe.

## Acceptance Criteria

- [ ] `core.Comment.External` parses and serialises in canonical key order and round-trips unchanged.
- [ ] Unknown comment front-matter keys still round-trip through `Extra`.
- [ ] `make wasm` still builds.
- [ ] Golden tests under `internal/core/testdata/` cover a comment with and without `external`; `go test -race ./internal/core/...` passes.
