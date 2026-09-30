---
id: GIT-US-0187
type: story
title: Guarantee managed Pando dies with serve on macOS
status: done
priority: medium
parent: GIT-EP-0031
assignees: [claude]
author: mcp
labels: [server, agent-ok]
created: 2026-09-30T07:00:52Z
updated: 2026-09-30T12:29:46Z
started: 2026-09-30T12:12:22Z
closed: 2026-09-30T12:29:46Z
---

## Description

ADR-039 open question 5, decided 2026-09-30: macOS must be supported with the same lifetime guarantee as Linux. Linux uses Pdeathsig; macOS has no equivalent, so if `gintrack serve` crashes the Pando child can be orphaned.

## Acceptance Criteria

- [x] On macOS the child is guaranteed to exit when its supervising `serve` dies, including on a crash (for example a parent-watch pipe, kqueue `NOTE_EXIT` on the parent pid, or a small wrapper), and an orphan left by a previous crash is detected and cleaned at the next start via the state file.
- [x] Tests cover supervisor death on darwin (they may be skipped when not on darwin) and orphan cleanup on all platforms.
- [x] CI builds for darwin; docs/21 states macOS support.

## Notes

Done in PR #113. Without Pdeathsig, the child runs under a watchdog, `gintrack __pando-watch`, which re-executes the same binary. The watchdog holds a pipe from the supervisor and kills the Pando process group on EOF. At start, an orphan found through `state.json` is reaped if its command line names the instance dir.

This was verified on Linux only: the watchdog path was forced there, including a SIGKILL of the supervisor. It has not been run on a real Mac; in particular, the `ps` command-line lookup on macOS is unexercised. CI now vets darwin and windows.
