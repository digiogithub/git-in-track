---
id: GIT-US-0159
type: story
title: Document impact blind spots and marker placement guidance
status: backlog
priority: low
parent: GIT-EP-0026
author: mcp
labels: [docs, agent-ok, good-first-issue]
estimate: 1
created: 2026-09-24T23:18:51Z
updated: 2026-09-24T23:18:51Z
---

## Description

The GIT-US-0137 benchmark (PR #76) showed which changes tiers 1–2 do not see: package-level declarations, removed calls, and code with no marker and not reachable through Pando. Agents and humans need guidance on where to put `Implements:` and `Verifies:` markers so that impact stays useful.

## Acceptance Criteria

- [ ] docs/08 §10.8 and the AGENTS.md SDD loop explain the blind spots and where to place markers (on functions that carry the behaviour, not only on entry points).
- [ ] The benchmark doc is linked as evidence.
