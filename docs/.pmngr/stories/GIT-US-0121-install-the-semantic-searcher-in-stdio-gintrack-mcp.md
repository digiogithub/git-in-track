---
id: GIT-US-0121
type: story
title: Install the semantic searcher in stdio gintrack mcp
status: todo
priority: high
parent: GIT-EP-0026
milestone: GIT-M-0015
author: claude
labels: [mcp, cli, agent-ok]
estimate: 2
created: 2026-09-24T12:11:03Z
updated: 2026-09-24T12:11:03Z
---

## Description

As an agent connected over stdio, I want `search_semantic` to work when Pando is configured. Today `cmd/gintrack/mcp.go` never calls `SetSemanticSearcher`, so over stdio it always answers `unavailable`; only `gintrack serve` wires it up. Prerequisite for every semantic tier of Phase 11.

## Acceptance Criteria

- [ ] Bug fix starts with a failing test proving stdio `search_semantic` answers `unavailable` although Pando is configured.
- [ ] `cmd/gintrack/mcp.go` builds and installs the same semantic searcher as `gintrack serve` (shared constructor, no duplicated wiring).
- [ ] Without Pando configured the tool still answers `unavailable` naming `search_items` as fallback.
- [ ] docs/08 no longer implies the stdio limitation; CHANGELOG entry under Unreleased.

## Notes

No dependency on the spec data model; can start immediately.
