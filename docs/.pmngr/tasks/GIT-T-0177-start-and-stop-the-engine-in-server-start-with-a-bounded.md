---
id: GIT-T-0177
type: task
title: Start and stop the engine in Server.Start with a bounded drain
status: todo
priority: medium
parent: GIT-US-0084
milestone: GIT-M-0011
author: mcp
labels: [server, agent-ok]
estimate: 3
created: 2026-09-13T13:19:35Z
updated: 2026-09-13T13:19:35Z
---

## Description

Construct the engine in `Server.Start` (`internal/server/server.go:289`) beside `startWatch` (`:299`) and `startTunnel` (`:303`), register the hub observer, and close it with `defer engine.Close(context.WithoutCancel(ctx))` next to `s.git.close` (`:308`). Shutdown flushes for a bounded grace period and then journals whatever remains. With no integration configured the engine starts idle and publishes nothing.

## Acceptance Criteria

- [ ] The engine starts with the server and stops on the same path, with `Close` idempotent.
- [ ] Shutdown drains within the grace period using a detached context, then journals the rest.
- [ ] A start/stop cycle with jobs in flight leaks no goroutines, proved by a test.
- [ ] Starting with no integration configured costs nothing measurable and emits no events.
