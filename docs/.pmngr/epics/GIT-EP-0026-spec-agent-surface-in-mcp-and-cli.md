---
id: GIT-EP-0026
type: epic
title: Spec agent surface in MCP and CLI
status: backlog
priority: high
milestone: GIT-M-0015
author: claude
labels: [mcp, cli, docs]
created: 2026-09-24T12:08:17Z
updated: 2026-09-24T12:08:17Z
links:
  - { kind: relates_to, target: GIT-T-0238 }
---

## Description

Expose the spec layer to agents and terminals. Prerequisite fix: the stdio `gintrack mcp` never installs the semantic searcher, so `search_semantic` over stdio always answers `unavailable`. Then: per-requirement MCP addressing (list/get/update one requirement with its block `rev`), `spec_context`, `spec_coverage`, `spec_impact`, `verify_requirement` and `trace_requirement`; the CLI `gintrack spec lint|impact|coverage|verify`; and the spec-driven agent loop documented in AGENTS.md and docs/08 §10.

## Acceptance Criteria

- [ ] `search_semantic` works over stdio when Pando is configured.
- [ ] Every requirement is individually addressable from MCP and the CLI.
- [ ] The documented tool counts (AGENTS.md, `cmd/gintrack/mcp.go`, docs/08) match `gintrack mcp --list-tools`.
- [ ] Every story of this epic is done.

## Notes

Design: [[research/2026-09-24-spec-driven-development-overview]] §5.
