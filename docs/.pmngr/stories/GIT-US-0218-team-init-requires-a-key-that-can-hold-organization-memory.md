---
id: GIT-US-0218
type: story
title: team init requires a key that can hold organization memory
status: backlog
priority: low
parent: GIT-EP-0034
author: claude-code
labels: [cli, core, agent-ok]
estimate: 2
created: 2026-11-08T22:30:00Z
updated: 2026-11-08T22:30:00Z
links:
  - { kind: blocked_by, target: GIT-US-0210 }
---

## Description

Organization memory ids carry the team key (ADR-044 §1), so a team key must also satisfy the
project-key grammar `[A-Z][A-Z0-9]{1,9}`. Today the team key grammar is wider
(`[A-Z][A-Z0-9-]{1,15}`, `internal/core/teamscaffold.go`), and a team whose key the project grammar
refuses gets `E-TEAM-KEY-MEMORY` on every memory file (GIT-US-0210). `gintrack init --team` and the
web app's "create team" should refuse such a key up front, or at least warn that the team cannot
hold organization memory.

## Acceptance Criteria

- [ ] Creating a team with a key outside `[A-Z][A-Z0-9]{1,9}` is refused, or warns clearly that the team cannot hold organization memory (decision for the maintainer).
- [ ] Existing teams with such keys keep working; only their memory folder is ignored.
- [ ] docs/04 states the constraint.

## Notes

Decided on 2026-11-08 while reviewing the US-0210 design: diagnostic now, enforcement as its own story.
