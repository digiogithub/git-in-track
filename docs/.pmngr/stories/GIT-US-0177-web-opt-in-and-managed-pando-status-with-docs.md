---
id: GIT-US-0177
type: story
title: Web opt-in and managed Pando status, with docs
status: done
priority: medium
parent: GIT-EP-0031
assignees: [claude]
author: mcp
labels: [web, docs, agent-ok]
created: 2026-09-29T18:53:52Z
updated: 2026-09-29T21:24:16Z
started: 2026-09-29T20:56:24Z
closed: 2026-09-29T21:24:16Z
---

## Description

User-facing side of ADR-039: the workspace list toggle saves the machine-local opt-in and the settings card shows managed state. Depends on the serve story of GIT-EP-0031.

## Acceptance Criteria

- [x] Enable/Disable semantic search in the workspace list saves `repos[].semanticSearch` and starts or stops the instance; Disable offers to delete the index as well.
- [x] The search settings card shows state, Pando version and last error, with a restart control; browser-only mode hides all of it.
- [x] Vitest + Testing Library cover the toggle and the card states.
- [x] docs/21 §1 and §7, docs/02 §8.1, docs/20 and docs/08 §10 updated, including how an agent connects to Pando's MCP directly with `gintrack pando status --json` and the warning that a direct connection exposes Pando's write tools.
