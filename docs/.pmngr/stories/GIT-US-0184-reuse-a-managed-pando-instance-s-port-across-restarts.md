---
id: GIT-US-0184
type: story
title: Reuse a managed Pando instance's port across restarts
status: done
priority: medium
parent: GIT-EP-0031
assignees: [claude]
author: mcp
labels: [server, agent-ok]
created: 2026-09-30T07:00:52Z
updated: 2026-09-30T12:42:43Z
started: 2026-09-30T12:30:05Z
closed: 2026-09-30T12:42:43Z
---

## Description

ADR-039 open question 2, decided 2026-09-30: keep loopback HTTP, and reuse the instance's port across restarts when it is free, so agents configured to connect to Pando's MCP directly do not break. Today the supervisor picks a new port on every (re)start.

## Acceptance Criteria

- [x] The supervisor stores the last port in `state.json` and tries it first on start and restart; if it is busy it falls back to a free port and records the change.
- [x] Pando silently moving to another port is still detected by the health check.
- [x] Tests cover port reused, port busy (fallback) and restart keeping the port.
- [x] docs/21 and docs/08 (direct agent connection) state the behaviour.

## Notes

Done in PR #114. `state.json` keeps `lastPort` across a stop. Start, a requested restart and a crash restart all try that port first, waiting up to 2 s (`Options.PortWait`) for the old socket to close. A busy or out-of-range port falls back to a free one and records `portChangedFrom`. A silent port move still counts as a crash, and that port is not preferred next time.
