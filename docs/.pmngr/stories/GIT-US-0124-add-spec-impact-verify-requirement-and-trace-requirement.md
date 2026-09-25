---
id: GIT-US-0124
type: story
title: Add spec_impact, verify_requirement and trace_requirement MCP tools
status: done
priority: high
parent: GIT-EP-0026
milestone: GIT-M-0015
author: claude
labels: [mcp, agent-ok]
estimate: 5
created: 2026-09-24T12:11:03Z
updated: 2026-09-25T11:15:00Z
closed: 2026-09-25T11:15:00Z
links:
  - { kind: blocked_by, target: GIT-US-0116 }
  - { kind: blocked_by, target: GIT-US-0120 }
  - { kind: blocked_by, target: GIT-US-0122 }
---

## Description

As an agent that has changed code, I want to ask which requirements my diff affects, stamp the ones my passing tests verify, and inspect the trace of one requirement.

## Acceptance Criteria

- [x] `spec_impact(base, head|worktree, budget, tiers?)` returns the compact impact report; `unavailable` (with tier-1 results when possible) without Pando or companion.
- [x] `verify_requirement(ref, rev)` (write tool) stamps `verified` only when all linked tests pass in the latest ingested results; otherwise it refuses with the failing tests.
- [x] `trace_requirement(ref)` returns code symbols, tests, stories and markers with their origin (marker vs `trace:`).
- [x] Tests in `internal/mcp` with fake seams; tool counts updated in `cmd/gintrack/mcp.go`, AGENTS.md and docs/08.

## Notes

Design: [[research/2026-09-24-spec-driven-development-overview]] §5 steps 3–5.
