---
id: GIT-T-0177
type: task
title: Start and stop the engine in Server.Start with a bounded drain
status: done
priority: medium
parent: GIT-US-0084
milestone: GIT-M-0011
author: mcp
labels: [server, agent-ok]
estimate: 3
created: 2026-09-13T13:19:35Z
updated: 2026-09-13T15:16:51Z
started: 2026-09-13T15:16:01Z
closed: 2026-09-13T15:16:51Z
---

## Description

Construct the engine in `Server.Start` (`internal/server/server.go:289`) beside `startWatch` (`:299`) and `startTunnel` (`:303`), register the hub observer, and close it with `defer engine.Close(context.WithoutCancel(ctx))` next to `s.git.close` (`:308`). Shutdown flushes for a bounded grace period and then journals whatever remains. With no integration configured the engine starts idle and publishes nothing.

## Acceptance Criteria

- [x] The engine starts with the server and stops on the same path, with `Close` idempotent.
- [x] Shutdown drains within the grace period using a detached context, then journals the rest.
- [x] A start/stop cycle with jobs in flight leaks no goroutines, proved by a test.
- [x] Starting with no integration configured costs nothing measurable and emits no events.

## Notes

The engine is **built in `New`** and **started in `Start`**, which is the one
point worth reviewing: `Engine.Start` replays the journal, so every handler must
be registered before it, and `New` is the only moment a caller holds the server
with nothing running. `Server.RegisterSyncHandler` is that registration point.

`Server.Start` calls `s.startSyncEngine(ctx)` after `startTunnel`, with
`defer s.stopSyncEngine(context.WithoutCancel(ctx))` beside `s.git.close`. The
stop gives the queue `DrainTimeout` (5 s) to finish, on the detached context, and
`Engine.Close` journals whatever did not. `Close` is idempotent through the
engine's own `closeOnce`. The leak test enqueues four jobs, closes, and asserts
both that all four ran and that the goroutine count came back down.
