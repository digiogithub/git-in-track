---
id: GIT-EP-0006
type: epic
title: MCP server and agent workflows
status: in_progress
created: 2026-09-03T00:00:00Z
updated: 2026-09-04
author: team
priority: high
milestone: GIT-M-0006
estimate: 16
labels: [mcp, core, docs]
links:
  - kind: blocked_by
    target: GIT-EP-0003
---

## Description

Phase 5. Make AI agents first-class collaborators. `gintrack mcp` speaks MCP over stdio,
and the companion server exposes the same tools over streamable HTTP. Responses are
optimised for agents: compact JSON, front matter only unless the body is requested, cursor
pagination, and a `rev` content hash on every item for optimistic locking.

The epic also defines the conventions that make agent contribution safe: how an agent
claims a story, what it may not touch, how it reports progress, and how its work is
attributed and reviewed.

## Acceptance Criteria

- [ ] An agent over MCP claims a `todo` story, moves it to `in_progress`, opens a PR and
      comments on the story, with no human file edits in the loop.
- [ ] Two agents writing the same item concurrently produce exactly one success and one
      `rev` conflict; no update is ever lost.
- [ ] Tool schemas are documented and stable; a schema change bumps the MINOR version.
- [ ] `AGENTS.md` is published and is the single reference for agent contributors.

## Notes

Stories: GIT-US-0024 … GIT-US-0026. See docs/11-roadmap.md, milestone 6.
Primary risk: R4 (ID collisions under parallel agent work).
