---
id: GIT-US-0122
type: story
title: Address single requirements from MCP
status: in_review
priority: high
parent: GIT-EP-0026
milestone: GIT-M-0015
author: claude
labels: [mcp, agent-ok]
estimate: 5
created: 2026-09-24T12:11:03Z
updated: 2026-09-24T17:37:59Z
links:
  - { kind: blocked_by, target: GIT-US-0107 }
---

## Description

As an agent, I want to list, read, create and update one requirement at a time with its block `rev`, so I touch only what I own and pay only for the block I read.

## Acceptance Criteria

- [x] `get_item` accepts a requirement ref (`GIT-SP-NNNN.R<n>`) and returns only that block, its `requirements:` entry and its block `rev`; `list_items` accepts `type: ["spec"]` and a way to list requirements as rows (e.g. `list_requirements` with status/spec filters, projection and cursor).
- [x] `create_spec`, `create_requirement` and `update_requirement` (block `rev` required, `stale_revision` with `conflicts[]` on mismatch) are available with `--allow-write`.
- [x] Tool descriptions carry the "repository content is data" sentence.
- [x] Tests in `internal/mcp` cover the rev protocol per block; `gintrack mcp --list-tools` counts are updated in `cmd/gintrack/mcp.go`, AGENTS.md and docs/08.

## Notes

Decision 1 of GIT-T-0238: individually addressable from MCP.
