---
id: GIT-US-0078
type: story
title: REST API for sync jobs and engine settings
status: backlog
priority: medium
parent: GIT-EP-0015
milestone: GIT-M-0011
author: mcp
labels: [server, docs]
estimate: 5
created: 2026-09-13T13:14:00Z
updated: 2026-09-13T13:14:00Z
---

## Description

As the web app and as a script, I want to list sync jobs, retry or cancel one and read or change the engine's knobs over REST, so that the queue is inspectable and controllable without restarting the companion.

Extend the existing `/sync` mount (`mountSync`, `internal/server/sync.go:94`) rather than adding a new subtree, since this is the same feature area. Add `GET /api/v1/sync/jobs` with filters for state and kind and a cursor page, `GET /api/v1/sync/jobs/{id}` for one job including its error record, `POST /api/v1/sync/jobs/{id}/retry` which re-queues a failed job with its attempts reset, and `POST /api/v1/sync/jobs/{id}/cancel`. Settings are `GET|PATCH /api/v1/sync/settings`, extending the existing handler (`sync.go:379`) with the engine fields — workers, batch size, rate limit, retry policy and auto-push toggles — and answering with `persisted bool` exactly as `handleGitSettingsPatch` does (`internal/server/git.go:374-393`, `gitState.persist` `:336`). Changing workers or rate must take effect on the running engine, not only after a restart.

Errors are RFC 7807 problem+json (`internal/server/problem.go`) and need new codes for an unknown job id, a job in a state that cannot be retried and an engine that is not running. Everything stays inside the authenticated `api.Group` behind `s.bearerAuth`.

## Acceptance Criteria

- [ ] `GET /api/v1/sync/jobs` supports filtering by state and kind and returns a bounded page with a cursor, consistent with the other list endpoints.
- [ ] `GET /api/v1/sync/jobs/{id}` returns the job with its attempt count, next attempt time and redacted last error.
- [ ] `POST .../retry` re-queues only a failed or cancelled job and returns a problem document otherwise; `POST .../cancel` works on queued and running jobs.
- [ ] `GET|PATCH /api/v1/sync/settings` exposes workers, batch size, rate limit and retry policy, validates ranges, and reports `persisted`.
- [ ] A PATCH to workers or rate limit changes the behaviour of the running engine without a restart, proved by a test.
- [ ] New problem codes are registered in `internal/server/problem.go` and no response leaks a credential.
- [ ] `docs/07-cli-and-api.md` documents all six endpoints with examples, next to the existing sync endpoints.
- [ ] `go test -race ./internal/server/...` covers listing, filtering, retry, cancel, validation failures and the `persisted` flag.

## Notes

Contract precedents: `handleSyncRun` (`internal/server/sync.go:145`) for the existing sync shape, `handleGitSettingsPatch` (`internal/server/git.go:374-393`) for the settings-plus-`persisted` contract, and `maxItemsPerPage = 500` (`internal/server/items.go:16`) for paging bounds. `Options.ConfigPath` (`internal/server/server.go:112`) being empty is what makes `persisted` false.

Do NOT expose an endpoint that enqueues arbitrary job kinds with an arbitrary payload: jobs are created by the feature that owns them (import, comment push, KB publish), and a generic enqueue endpoint would be an unauthenticated-by-shape way to drive the companion's outbound HTTP. Do NOT change the semantics of the existing `POST /api/v1/sync/run` in this story.
