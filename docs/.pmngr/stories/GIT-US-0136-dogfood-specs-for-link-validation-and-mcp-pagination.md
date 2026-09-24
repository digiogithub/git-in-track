---
id: GIT-US-0136
type: story
title: Dogfood specs for link validation and MCP pagination
status: backlog
priority: medium
parent: GIT-EP-0029
milestone: GIT-M-0015
author: claude
labels: [docs, mcp, agent-ok]
estimate: 3
created: 2026-09-24T12:11:59Z
updated: 2026-09-24T12:11:59Z
links:
  - { kind: blocked_by, target: GIT-US-0108 }
  - { kind: blocked_by, target: GIT-US-0113 }
  - { kind: blocked_by, target: GIT-US-0116 }
  - { kind: blocked_by, target: GIT-US-0125 }
---

## Description

As the project, we want link validation and MCP list pagination described as specs with traced requirements.

## Acceptance Criteria

- [ ] Two specs under `docs/.pmngr/specs/`: link validation (kinds, inverses, requirement-ref targets, dangling targets) and MCP pagination (limit bounds, nextCursor, filter stability across a walk, projection).
- [ ] Every requirement passes the grammar linter with no warnings and has at least one scenario.
- [ ] Markers added in `internal/core/validate.go`, `internal/mcp` and their tests; `gintrack spec coverage` shows no `untested` requirement.
- [ ] `make test` and `make lint` pass.

## Notes

Together with the rev/ID specs this gives four dogfood capabilities.
