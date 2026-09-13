---
id: GIT-T-0159
type: task
title: Extract a Searcher contract in internal/core
status: todo
priority: medium
parent: GIT-US-0082
milestone: GIT-M-0013
author: mcp
labels: [core, agent-ok]
estimate: 2
created: 2026-09-13T13:19:10Z
updated: 2026-09-13T13:19:10Z
---

## Description

Define a `Searcher` interface in `internal/core` matching the existing `Index.Search(q string, limit int) []SearchHit` signature (`internal/core/query.go:518`, `:544`) so `*core.Index` satisfies it with no behaviour change, and widen `SearchHit` with an origin field defaulted to the core backend. The interface and the type must stay free of `net/http` and of anything else that breaks the WASM build.

## Acceptance Criteria

- [ ] `*core.Index` satisfies `Searcher` with no changes to its search behaviour.
- [ ] `SearchHit` carries an origin that defaults to the core backend for existing callers.
- [ ] `make wasm` builds and `go test -race ./internal/core/...` passes unchanged.
