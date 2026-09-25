---
id: GIT-EP-0025
type: epic
title: Requirement impact analysis over Pando
status: backlog
priority: high
milestone: GIT-M-0015
author: claude
labels: [server, mcp]
created: 2026-09-24T12:08:11Z
updated: 2026-09-24T12:08:11Z
links:
  - { kind: relates_to, target: GIT-T-0238 }
---

## Description

Given a diff, return the affected requirements with the reason for each hit, in a few hundred tokens. Three tiers, cheapest first: (1) direct trace — changed files/symbols carrying a marker or declared in `trace:`; (2) transitive — Pando `code_impact_analysis` over changed symbols reaching marked callers; (3) semantic — Pando search over requirement blocks, flagged `candidate` with a score.

Needs new wrappers in `internal/pando/search.go` (`code_impact_analysis`, `code_find_symbol`, `code_related_files`) and requirement blocks indexed in Pando so hits resolve to the block anchor. Pando's code index skips dot-dirs, so `.pmngr` must be fed explicitly; Pando never writes specs. Without Pando, tiers 2–3 answer `unavailable` and tier 1 still works.

## Acceptance Criteria

- [ ] Tiers 1 and 2 are deterministic for a given diff.
- [ ] A typical PR's impact report fits in ≤ 1.5k tokens under the default budget.
- [ ] Without Pando the resolver degrades to tier 1 plus an explicit `unavailable` marker.
- [ ] Every story of this epic is done.

## Notes

Design: [[research/2026-09-24-spec-driven-development-overview]] §2. Pando code stays out of `internal/core` and `internal/vault` (WASM).
