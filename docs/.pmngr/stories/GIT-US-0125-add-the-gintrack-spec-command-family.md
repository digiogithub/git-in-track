---
id: GIT-US-0125
type: story
title: Add the gintrack spec command family
status: done
priority: high
parent: GIT-EP-0026
milestone: GIT-M-0015
author: claude
labels: [cli, agent-ok]
estimate: 5
created: 2026-09-24T12:11:04Z
updated: 2026-09-25T11:15:00Z
closed: 2026-09-25T11:15:00Z
links:
  - { kind: blocked_by, target: GIT-US-0108 }
  - { kind: blocked_by, target: GIT-US-0115 }
  - { kind: blocked_by, target: GIT-US-0116 }
  - { kind: blocked_by, target: GIT-US-0120 }
---

## Description

As a developer in a terminal or CI, I want `gintrack spec lint|impact|coverage|verify|trace`, so the same answers the MCP tools give are scriptable.

## Acceptance Criteria

- [x] `cmd/gintrack/spec.go` (thin cobra commands) adds `lint`, `impact --since <ref> [--head <ref>] [--budget n] [--fail-on failing,suspect]`, `coverage [--status ...]`, `verify <ref>...` and `trace <ref>`, each with `--json`.
- [x] `--fail-on` sets a non-zero exit code when any hit has a listed status; lint exits non-zero only for `error` findings.
- [x] No domain logic in `cmd/`; commands call the vault and the native seams.
- [x] Tests for flag parsing and exit codes; docs/07 documents every command.

## Notes

The CI gate epic builds on `impact --fail-on`.
