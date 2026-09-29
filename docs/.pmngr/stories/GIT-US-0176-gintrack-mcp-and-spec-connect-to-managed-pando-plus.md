---
id: GIT-US-0176
type: story
title: gintrack mcp and spec connect to managed Pando, plus gintrack pando command
status: done
priority: medium
parent: GIT-EP-0031
assignees: [claude]
author: mcp
labels: [cli, mcp, agent-ok]
created: 2026-09-29T18:53:52Z
updated: 2026-09-29T21:24:16Z
started: 2026-09-29T20:56:24Z
closed: 2026-09-29T21:24:16Z
---

## Description

Per the maintainer decision in ADR-039, `gintrack mcp` never starts Pando nor proxies its tools. `gintrack mcp` and `gintrack spec` only connect to an instance that `gintrack serve` is running; agents wanting Pando's own tools connect to Pando's MCP directly.

## Acceptance Criteria

- [x] `gintrack mcp` and `gintrack spec` discover a running instance via `state.json`, the 0600 token file and a health check; with none they answer `unavailable` with "managed Pando is not running — start `gintrack serve`".
- [x] `gintrack pando status [--json] [--repo]` works without a server and prints `mcpUrl` and `tokenFile`, never the token.
- [x] `gintrack pando start|stop|restart|reset` act through the running `serve`.
- [x] Tests cover found, stale state file and not running.
