---
id: GIT-T-0146
type: task
title: Serve the extended close and the new transfer endpoint
status: done
priority: medium
parent: GIT-US-0085
milestone: GIT-M-0012
author: mcp
labels: [server]
estimate: 3
created: 2026-09-13T13:18:52Z
updated: 2026-09-13T15:17:45Z
started: 2026-09-13T15:17:19Z
closed: 2026-09-13T15:17:45Z
---

## Description

Extend `POST /api/v1/sprints/{id}/close` with the `transfer` and `dryRun` fields and add `POST /api/v1/sprints/{id}/transfer` in `internal/server/sprints.go`, both requiring `If-Match` on the sprint and forwarding through `s.call(...)`. Publish a `sprint.changed` event and the usual `item.changed` events for every item touched, using `publishWriteSets` (`internal/server/events.go:338`); a dry run publishes nothing.

## Acceptance Criteria

- [x] Both routes answer the documented shapes, require `If-Match` and map a stale rev to 412.
- [x] A dry run writes nothing and publishes nothing.
- [x] `sprint.changed` and per-item events are published on a real transfer and documented in `docs/07-cli-and-api.md`.
- [x] `go test -race ./internal/server/...` covers both routes, the dry run and the event.

## Notes

`publishSprintWrite` now returns early on `result.DryRun` — gated on the flag,
**not** on an empty write set, so that the intent is visible in the code and a
preview that happened to write nothing is never confused with a commitment that
did. Nothing at all is published on a dry run: no file event, no index refresh,
no commit-on-save, no `sprint.changed`, no `item.changed`.

`sprint.changed` carries `{sprint, board, state, carried, failed}` and is
followed by one `item.changed` per reference that actually moved; a reference
whose project this machine has not cloned is counted in `failed` and skipped,
because there is no local item to refresh. `sprint_target_completed` is
registered in `problem.go` as a 409.

Documented in docs/07 §5.5 (both routes, with a worked transfer example and the
`repo_not_cloned` per-item line) and §5.6 (`sprint.changed`).
