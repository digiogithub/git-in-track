---
id: GIT-T-0146
type: task
title: Serve the extended close and the new transfer endpoint
status: todo
priority: medium
parent: GIT-US-0085
milestone: GIT-M-0012
author: mcp
labels: [server]
estimate: 3
created: 2026-09-13T13:18:52Z
updated: 2026-09-13T13:18:52Z
---

## Description

Extend `POST /api/v1/sprints/{id}/close` with the `transfer` and `dryRun` fields and add `POST /api/v1/sprints/{id}/transfer` in `internal/server/sprints.go`, both requiring `If-Match` on the sprint and forwarding through `s.call(...)`. Publish a `sprint.changed` event and the usual `item.changed` events for every item touched, using `publishWriteSets` (`internal/server/events.go:338`); a dry run publishes nothing.

## Acceptance Criteria

- [ ] Both routes answer the documented shapes, require `If-Match` and map a stale rev to 412.
- [ ] A dry run writes nothing and publishes nothing.
- [ ] `sprint.changed` and per-item events are published on a real transfer and documented in `docs/07-cli-and-api.md`.
- [ ] `go test -race ./internal/server/...` covers both routes, the dry run and the event.
