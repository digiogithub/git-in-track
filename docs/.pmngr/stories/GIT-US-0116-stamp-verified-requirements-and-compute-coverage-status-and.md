---
id: GIT-US-0116
type: story
title: Stamp verified requirements and compute coverage status and suspect
status: done
priority: high
parent: GIT-EP-0024
milestone: GIT-M-0015
author: claude
labels: [server, git, agent-ok]
estimate: 5
created: 2026-09-24T12:10:07Z
updated: 2026-09-25T11:15:00Z
closed: 2026-09-25T11:15:00Z
links:
  - { kind: blocked_by, target: GIT-US-0107 }
  - { kind: blocked_by, target: GIT-US-0112 }
  - { kind: blocked_by, target: GIT-US-0115 }
---

## Description

As a reviewer, I want each requirement to show `untested`, `passing`, `failing` or `suspect`, and a stamp recording when it was last verified, so drift is caught deterministically without re-reading code.

## Acceptance Criteria

- [x] Verifying a requirement whose linked tests all pass writes `requirements.R<n>.verified: {rev, commit, at, by}` through `UpdateRequirement` (block-rev protected); nothing else is written back.
- [x] Status is computed: `untested` (no tests traced or no results), `failing` (any linked test failed), `suspect` (block `rev` ≠ `verified.rev`, or traced files/symbols changed between `verified.commit` and HEAD per `gitops.ChangedFiles`), else `passing`.
- [x] `suspect` is never stored in a file.
- [x] A coverage service returns one row per requirement with status, reasons and linked tests, installed through a host seam; browser-only answers `unavailable`.
- [x] Table-driven tests over a fixture repository covering each status transition; docs/03 documents the rules.

## Notes

Transitive suspect (through Pando) is added by the impact resolver.
