---
id: GIT-US-0030
type: story
title: Ship the 1.0 release
status: backlog
created: 2026-09-03T00:00:00Z
updated: 2026-09-03T00:00:00Z
author: team
priority: high
parent: GIT-EP-0007
milestone: GIT-M-0007
estimate: 3
labels: [ci, docs]
links:
  - kind: blocked_by
    target: GIT-US-0028
  - kind: blocked_by
    target: GIT-US-0029
---

## Description

As the project, I want a 1.0 that people can rely on, so that adopting git-in-track is a
reasonable decision for a team's real backlog rather than an experiment.

This is the release story: freeze the on-disk data model at `schemaVersion: 1`, publish the
compatibility promise, provide and test `gintrack migrate` from every 0.x layout, complete
the user documentation, run the release checklist from
docs/09-ci-cd-and-releases.md §9, and tag `v1.0.0`.

It also closes the dogfooding loop: this project's own backlog under `docs/.pmngr/` must
have been planned, run and retrospected in the tool, because if that was not comfortable,
we are not ready to ask anyone else to do it.

## Acceptance Criteria

- [ ] The data model is frozen at `schemaVersion: 1` and the promise is documented.
- [ ] `gintrack migrate` upgrades every 0.x vault layout, with tests per version.
- [ ] The full release checklist is completed and recorded.
- [ ] User documentation covers installation, both modes, teams, sync, MCP and metrics.
- [ ] `v1.0.0` publishes six archives, `checksums.txt` and a complete changelog.
- [ ] Every artifact is verified against its checksum on a clean machine per platform.
- [ ] The unsigned-binary bypass instructions are verified on macOS and Windows.
- [ ] This project's own backlog has been run in the tool through at least one full sprint.
- [ ] All Phase 0–6 milestones are closed with their stories `done` or `cancelled`.
