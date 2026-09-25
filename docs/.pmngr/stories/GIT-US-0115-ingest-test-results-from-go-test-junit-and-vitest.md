---
id: GIT-US-0115
type: story
title: Ingest test results from go test, JUnit and Vitest
status: backlog
priority: high
parent: GIT-EP-0024
milestone: GIT-M-0015
author: claude
labels: [server, cli, agent-ok]
estimate: 5
created: 2026-09-24T12:10:07Z
updated: 2026-09-24T12:10:07Z
links:
  - { kind: blocked_by, target: GIT-US-0114 }
---

## Description

As a maintainer, I want the last result of every test that verifies a requirement, so coverage says `passing` or `failing` from real runs instead of an LLM guess.

## Acceptance Criteria

- [ ] Parsers for `go test -json`, JUnit XML and Vitest JSON reporter output produce `{test id, path#Name, result, duration, commit}`.
- [ ] `gintrack spec ingest <file>...` (or a flag on `spec verify`) stores results in a derived cache outside the repository's source of truth (rebuildable, ignored by git).
- [ ] Test IDs are matched to trace entries (`path#TestName`, including subtests `TestX/case`) and `Verifies:` markers.
- [ ] Fixture-based tests for each format including failures, skips and subtests.
- [ ] docs/07 documents the command and supported formats.

## Notes

Native only.
