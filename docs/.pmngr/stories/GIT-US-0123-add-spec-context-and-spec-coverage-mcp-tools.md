---
id: GIT-US-0123
type: story
title: Add spec_context and spec_coverage MCP tools
status: backlog
priority: high
parent: GIT-EP-0026
milestone: GIT-M-0015
author: claude
labels: [mcp, agent-ok]
estimate: 5
created: 2026-09-24T12:11:03Z
updated: 2026-09-24T12:11:03Z
links:
  - { kind: blocked_by, target: GIT-US-0116 }
  - { kind: blocked_by, target: GIT-US-0122 }
---

## Description

As an agent picking up a story, I want the requirements it implements or modifies, their statements, scenarios and test status, within a token budget — generated on demand, never copied into the story.

## Acceptance Criteria

- [ ] `spec_context(story, budget)` returns linked requirements (via `implements`/`modifies` and the Spec Delta) with one-line statements, scenarios, coverage status and related KB pages, truncated to budget with a cursor.
- [ ] `spec_coverage(spec?, status?)` returns one row per requirement with `untested`/`passing`/`failing`/`suspect`, reasons and linked tests; `unavailable` when the host did not install the coverage seam.
- [ ] Read-only tools, present without `--allow-write`.
- [ ] Tests in `internal/mcp`; docs/08 tool reference and counts updated.

## Notes

Design: [[research/2026-09-24-spec-driven-development-overview]] §5 step 1.
