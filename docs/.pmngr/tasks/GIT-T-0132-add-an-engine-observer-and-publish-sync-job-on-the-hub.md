---
id: GIT-T-0132
type: task
title: Add an engine observer and publish sync.job.* on the hub
status: todo
priority: medium
parent: GIT-US-0074
milestone: GIT-M-0011
author: mcp
labels: [server, agent-ok]
estimate: 3
created: 2026-09-13T13:18:32Z
updated: 2026-09-13T13:18:32Z
---

## Description

Add an `Observer` seam to `internal/syncengine` receiving state transitions and progress, and implement it in `internal/server` publishing `sync.job.queued|started|progress|done|failed` through `Hub.Publish` (`internal/server/hub.go:167`), alongside the existing publishers in `internal/server/events.go`. Payloads carry job id, kind, key, state, attempt, processed and total counts, and the redacted error on failure. The engine must not import `internal/server`.

## Acceptance Criteria

- [ ] All five topics are published with the documented payload and no credential ever appears in one.
- [ ] `internal/syncengine` has no dependency on `internal/server`, enforced by the package's imports.
- [ ] `go test -race ./internal/server/...` asserts one event per transition against a fake engine.
