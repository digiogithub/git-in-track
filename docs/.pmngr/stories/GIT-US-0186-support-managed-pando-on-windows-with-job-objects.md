---
id: GIT-US-0186
type: story
title: Support managed Pando on Windows with Job Objects
status: backlog
priority: low
parent: GIT-EP-0031
author: mcp
labels: [server, agent-ok]
created: 2026-09-30T07:00:52Z
updated: 2026-09-30T07:00:52Z
---

## Description

ADR-039 open question 5, decided 2026-09-30: managed mode should work on Windows. The supervisor compiles there but refuses managed mode, because nothing ties the child's lifetime to gintrack.

## Acceptance Criteria

- [ ] On Windows the child runs in a Job Object with kill-on-job-close, so it dies with `gintrack serve` even on a crash; the lock and state file work on Windows.
- [ ] Stop and restart terminate the process tree cleanly.
- [ ] CI builds and runs the supervisor tests on a Windows runner (or the story documents why not and adds a build-tag test).
- [ ] docs/21 and docs/09 state Windows support.

## Notes

Justify any new dependency (for example `golang.org/x/sys/windows`) in the PR, as AGENTS.md requires.
