---
id: GIT-US-0209
type: story
title: Grant-checking wrapper around the vault Dispatcher for the hub
status: todo
priority: high
parent: GIT-EP-0034
author: claude-code
labels: [server, security, agent-ok]
estimate: 5
created: 2026-11-08T17:59:20Z
updated: 2026-11-08T17:59:20Z
links:
  - { kind: blocked_by, target: GIT-US-0208 }
---

## Description

One authorisation point shared by REST, MCP and the memory protocol (ADR-043 §4): a Dispatcher wrapper that admits a call only if the principal holds the needed grant (`read`, `propose`, `write`, `delete`) on the project or scope it touches, and injects the granted projects into search scopes so an unscoped query never crosses tenants. Grants live in YAML. Human-only area: design approved before code.

## Acceptance Criteria

- [ ] Design approved by the supervisor before implementation.
- [ ] Every Dispatcher method is classified (read/write/admin) and a test walks all of them with a principal holding no grant: none succeeds or leaks a row.
- [ ] Search, semantic search, KPIs and graph results are restricted to granted projects.
- [ ] `propose` without `write` can only create memory in `proposed`.
- [ ] Unknown methods are denied by default.

## Notes

Part of the gintrack hub PoC; see ADR-043 and ADR-044.
