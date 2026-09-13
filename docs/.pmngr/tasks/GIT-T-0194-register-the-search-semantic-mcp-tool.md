---
id: GIT-T-0194
type: task
title: Register the search_semantic MCP tool
status: todo
priority: medium
parent: GIT-US-0088
milestone: GIT-M-0013
author: mcp
labels: [mcp]
estimate: 3
created: 2026-09-13T13:20:05Z
updated: 2026-09-13T13:20:05Z
---

## Description

Add `internal/mcp/tools_search.go` with the `In` and `Out` structs carrying `json` and `jsonschema` tags, a `registerSearchTools(s)` function called from `registerTools` (`internal/mcp/tools.go:24`), and a single `register(s, toolDef{Name: "search_semantic", Untrusted: true}, handler)` call. It is a read tool, so it is advertised without `--allow-write`. The handler validates arguments with `invalidField`, dispatches through `dispatch[T]`, and projects results through `internal/mcp/wire.go`. No business logic in this package.

## Acceptance Criteria

- [ ] The tool appears in `tools/list` on a read-only server with a schema inferred from the structs.
- [ ] The untrusted-content note is appended to its description.
- [ ] The unavailable-backend case returns a structured tool error.
- [ ] A behaviour test through the in-memory client harness covers a hit, an empty result and the unavailable backend.
