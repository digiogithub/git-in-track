---
id: GIT-US-0135
type: story
title: Dogfood specs for the rev protocol and ID allocation
status: backlog
priority: medium
parent: GIT-EP-0029
milestone: GIT-M-0015
author: claude
labels: [docs, core, agent-ok]
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

As the project, we want our own `rev` write protocol and ID allocation described as specs with traced requirements, so Phase 11 is proven on real code.

## Acceptance Criteria

- [ ] Two specs under `docs/.pmngr/specs/` (created through the tools): the `rev` protocol (stale_revision, conflicts[], empty-conflict no-op, no `*` escape) and ID allocation (next = max+1, permanence, gaps, requirement `R<n>`).
- [ ] Every requirement passes the grammar linter with no warnings and has at least one scenario.
- [ ] `// Implements:` and `// Verifies:` markers added in `internal/core`, `internal/vault` and `internal/mcp` code and tests; `gintrack spec coverage` shows no `untested` requirement.
- [ ] Markers are comment-only changes; `make test` and `make lint` pass.

## Notes

Pick-up order after the marker scanner and linter land.
