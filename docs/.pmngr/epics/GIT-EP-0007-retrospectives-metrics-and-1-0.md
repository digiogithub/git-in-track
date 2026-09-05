---
id: GIT-EP-0007
type: epic
title: Retrospectives, metrics and 1.0
status: in_progress
priority: high
milestone: GIT-M-0007
author: team
labels: [web, core, ci, docs]
estimate: 21
created: 2026-09-03T00:00:00Z
updated: 2026-09-04T00:00:00Z
links:
  - { kind: blocked_by, target: GIT-EP-0004 }
  - { kind: blocked_by, target: GIT-EP-0005 }
  - { kind: blocked_by, target: GIT-EP-0006 }
---

## Description

Phase 6. Close the agile loop and ship 1.0. Retrospectives are stored as Markdown in the
team repository, improvement actions can be promoted into real tasks in project
repositories, and metrics (burndown, cumulative flow, cycle and lead time, throughput) are
derived from the index and git history rather than stored.

The epic also covers the polish pass and the distribution channels beyond the GitHub
Release, and freezes the on-disk data model at `schemaVersion: 1` with a written
compatibility promise.

## Acceptance Criteria

- [ ] A full sprint is planned, run, closed and retrospected inside the tool by this
      project's own team, using `docs/.pmngr/`.
- [x] Burndown and cumulative flow match a hand-computed reference for a fixture sprint.
- [ ] Primary flows pass WCAG 2.1 AA checks.
- [ ] `brew install`, `scoop install`, `docker run` and `go install` each yield a working
      `gintrack` on a clean machine.
- [ ] The data model is frozen at `schemaVersion: 1` with a documented 0.x migration path.

## Notes

Stories: GIT-US-0027 … GIT-US-0030. See docs/11-roadmap.md, milestone 7.
