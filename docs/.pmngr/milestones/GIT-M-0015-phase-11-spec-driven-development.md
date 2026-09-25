---
id: GIT-M-0015
type: milestone
title: Phase 11 — Spec-driven development
status: backlog
author: claude
created: 2026-09-24T12:07:44Z
updated: 2026-09-24T12:07:44Z
links:
  - { kind: relates_to, target: GIT-T-0238 }
---

## Description

Add a spec layer to git-in-track so agents and humans can answer three questions cheaply and deterministically: which requirements does this change affect (impact), is each requirement tested and passing (coverage), and does it still hold after the change (validity / suspect).

A new `spec` item (`GIT-SP-NNNN`, `docs/.pmngr/specs/`) holds its requirements as blocks `### GIT-SP-NNNN.R<n> — <title>`, each an addressable unit with its own status, block `rev`, trace, verification stamp and Pando anchor. Requirements trace to code and tests through `// Implements:` / `// Verifies:` markers and an optional `trace:` entry. A three-tier impact query (direct trace, Pando transitive impact, semantic candidates) is exposed through MCP, the CLI, the web app and a CI gate.

Design and decisions: [[research/2026-09-24-spec-driven-development-overview]] (validated through GIT-T-0238).

## Acceptance Criteria

- [ ] The impact report for a typical PR is ≤ 1.5k tokens and at least 10× cheaper than reading the relevant spec folder (measured in the dogfood epic).
- [ ] Impact tiers 1 and 2 are deterministic: the same diff gives the same result, with or without an LLM.
- [ ] Every requirement shows one of `untested`, `passing`, `failing` or `suspect` in the CLI, MCP and web (including the coverage matrix).
- [ ] `make wasm` passes; browser-only mode keeps authoring and lint, while impact and ingest answer `unavailable`.
- [ ] `gintrack spec impact --since <ref> --fail-on failing,suspect` runs as a PR check.
- [ ] Every epic of this milestone is done.

## Notes

Planned 2026-09-24. E1 (data model) is a human-supervised area: ADR-037 and docs/03 are approved before any code. Spec importers (Spec Kit, OpenSpec, Kiro) are a separate epic with no milestone.
