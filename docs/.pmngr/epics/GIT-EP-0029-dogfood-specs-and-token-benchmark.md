---
id: GIT-EP-0029
type: epic
title: Dogfood specs and token benchmark
status: backlog
priority: medium
milestone: GIT-M-0015
author: claude
labels: [docs]
created: 2026-09-24T12:08:35Z
updated: 2026-09-24T12:08:35Z
links:
  - { kind: relates_to, target: GIT-T-0238 }
---

## Description

Use the spec layer on git-in-track itself. Write specs for four of our own capabilities — the `rev` write protocol, ID allocation, link validation and MCP pagination — with markers in the code and tests, then measure the tokens an agent spends answering "what does this PR affect?" with `spec_impact` against reading the relevant spec folder.

## Acceptance Criteria

- [ ] Four dogfood specs exist under `docs/.pmngr/specs/`, lint clean, with every requirement traced.
- [ ] The benchmark shows the impact report ≤ 1.5k tokens and ≥ 10× cheaper for a typical PR, recorded under `docs/research/`.
- [ ] Every story of this epic is done.

## Notes

Design: [[research/2026-09-24-spec-driven-development-overview]] §6.
