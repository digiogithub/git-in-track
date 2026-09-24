---
id: GIT-US-0126
type: story
title: Document the spec-driven agent loop in AGENTS.md and docs/08
status: in_review
priority: medium
parent: GIT-EP-0026
milestone: GIT-M-0015
author: claude
labels: [docs, agent-ok]
estimate: 2
created: 2026-09-24T12:11:04Z
updated: 2026-09-24T21:06:28Z
links:
  - { kind: blocked_by, target: GIT-US-0123 }
  - { kind: blocked_by, target: GIT-US-0124 }
  - { kind: blocked_by, target: GIT-US-0125 }
---

## Description

As an agent new to the repository, I want the spec-driven loop spelled out where I already look, so I call `spec_context` before coding and `spec_impact` before opening a PR.

## Acceptance Criteria

- [x] AGENTS.md gains the SDD loop (context → implement with markers → impact → fix / Spec Delta / comment → verify → PR) and the updated tool list and counts.
- [x] docs/08 §10 covers editing specs and requirement blocks directly, including the block-level `rev` equivalent (hash the block bytes).
- [x] docs/08 tool reference lists every spec tool; README mentions specs.
- [x] `gintrack mcp --list-tools` output matches the documented counts (checked by an existing or new test).

## Notes

Closes the agent-surface epic.
