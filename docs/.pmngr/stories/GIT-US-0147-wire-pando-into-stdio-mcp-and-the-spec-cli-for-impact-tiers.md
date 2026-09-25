---
id: GIT-US-0147
type: story
title: Wire Pando into stdio MCP and the spec CLI for impact tiers 2 and 3
status: done
priority: high
parent: GIT-EP-0026
milestone: GIT-M-0015
author: mcp
labels: [mcp, cli, agent-ok]
estimate: 3
created: 2026-09-24T20:35:42Z
updated: 2026-09-25T11:15:00Z
closed: 2026-09-25T11:15:00Z
---

## Description

`spec_impact` over stdio `gintrack mcp` and `gintrack spec impact` report tiers 2 and 3 as `unavailable`: only `gintrack serve` gives the impact seam a Pando code-graph client and a semantic searcher. Agents mostly connect over stdio, so they lose the transitive and semantic tiers. GIT-US-0121 (#26) installed the semantic searcher for stdio, but it is not in the Phase 11 stack.

## Acceptance Criteria

- [x] GIT-US-0121 is rebased into the Phase 11 stack, with its `cmd/gintrack/mcp.go` conflicts resolved.
- [x] Stdio `gintrack mcp` and `gintrack spec impact` build the same Pando client and semantic searcher as `serve`, through one shared constructor, and pass them to the impact seam (`CallGraph` / `Semantic`).
- [x] Without Pando configured, behaviour is unchanged: tier 1 answers, and tiers 2 and 3 are `unavailable`.
- [x] Tests with the fake Pando server show tiers 2 and 3 answering over stdio and through the CLI. `make lint`, `make test` and `make wasm` pass.
- [x] docs/08 and docs/07 are updated.

## Notes

Found while reviewing GIT-US-0125 (#48).
