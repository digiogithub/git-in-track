---
id: GIT-M-0006
type: milestone
title: Phase 5 — MCP server and agent workflows
status: done
author: team
labels: [mcp, docs]
created: 2026-09-03T00:00:00Z
updated: 2026-09-07T07:14:24Z
started: 2026-09-07T07:14:16Z
closed: 2026-09-07T07:14:24Z
links:
  - { kind: relates_to, target: GIT-EP-0006 }
custom:
  phase: 5
  version: v0.6.0
---

## Description

Phase 5 of the roadmap. An MCP server over stdio and streamable HTTP exposing the backlog
and knowledge base to AI agents, with compact agent-optimised responses, pagination and
rev-based optimistic locking, plus the AGENTS.md conventions for safe agent contribution.

## Acceptance Criteria

- [x] An agent claims a story, works it and comments, with no human file edits.
- [x] Concurrent agent writes yield one success and one `rev` conflict.
- [x] Tool schemas documented and versioned.
- [x] `AGENTS.md` published and referenced from the README.

## Notes

Epic: GIT-EP-0006. Stories: GIT-US-0024 … GIT-US-0026. Risk: R4.
