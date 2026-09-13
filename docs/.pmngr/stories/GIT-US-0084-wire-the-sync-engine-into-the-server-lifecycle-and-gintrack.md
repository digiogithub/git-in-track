---
id: GIT-US-0084
type: story
title: Wire the sync engine into the server lifecycle and gintrack serve
status: backlog
priority: high
parent: GIT-EP-0015
milestone: GIT-M-0011
author: mcp
labels: [server, cli, docs]
estimate: 3
created: 2026-09-13T13:14:30Z
updated: 2026-09-13T13:14:30Z
---

## Description

As an operator, I want the sync engine to start with the companion, drain on shutdown and be configurable from the command line, so that stopping `gintrack serve` never abandons half-finished work and a headless deployment can be tuned without a UI.

Construct the engine in `Server.Start` (`internal/server/server.go:289`) beside `startWatch` (`:299`) and `startTunnel` (`:303`), register the observer that publishes `sync.job.*`, and shut it down with `defer engine.Close(context.WithoutCancel(ctx))` next to `s.git.close` (`:308`) — the detached context is what lets an in-flight job finish instead of being cancelled by the very shutdown that is waiting for it. Shutdown gets a bounded drain: flush for up to a configured grace period, then persist whatever is still queued to the journal and stop. Add the engine's configuration to `server.Options` (`:59-131`) next to `Git config.Git` and `Tunnel config.Tunnel`, and to `internal/config/config.go` so it is loadable from the config file.

Expose the knobs on `gintrack serve` as `--sync-workers`, `--sync-batch`, `--sync-rate` and `--sync-max-attempts`, registered through `config.Resolve`/`applyFlags` (`internal/config/load.go:146`, `:224`) so the documented flag > env > file > default precedence holds. The engine must also be safely absent: when no integration is configured it starts idle and costs nothing, and every surface behaves as if the queue is simply empty.

## Acceptance Criteria

- [ ] The engine is created in `Server.Start`, shut down on the same path as the other background components, and `Close` is idempotent.
- [ ] Shutdown drains the queue for a bounded grace period with `context.WithoutCancel`, then journals the remainder; no goroutine leaks, proved by a test.
- [ ] Engine settings exist in `server.Options` and in `internal/config`, with validated defaults of 2 workers, batch 20, 5 req/s and 5 attempts.
- [ ] `gintrack serve` accepts `--sync-workers`, `--sync-batch`, `--sync-rate` and `--sync-max-attempts`, and flag > env > file > default precedence is covered by a test.
- [ ] With no integration configured the engine starts idle, publishes nothing and adds no measurable start-up cost.
- [ ] `docs/07-cli-and-api.md` documents the new flags and config keys, and `CHANGELOG.md` records the new background component.
- [ ] `go test -race ./internal/server/... ./cmd/...` passes, including a start/stop cycle with jobs in flight.

## Notes

Lifecycle precedents: `startWatch` (`internal/server/watch.go:64`) and the tunnel driver (`internal/server/tunnel.go`, detached context at `:198`). The companion runs seven non-test goroutines today, so any new one is easy to spot in a leak test.

Do NOT start the engine from a request handler or lazily on first use: a component with a shutdown contract belongs in `Server.Start`. Do NOT block `Server.Start` on a network call — the engine comes up whether or not YouTrack is reachable.
