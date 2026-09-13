---
id: GIT-T-0129
type: task
title: Prefer the stored snapshot over walking git in sprint metrics
status: todo
priority: medium
parent: GIT-US-0080
milestone: GIT-M-0012
author: mcp
labels: [core, server]
estimate: 3
created: 2026-09-13T13:18:29Z
updated: 2026-09-13T13:18:29Z
---

## Description

Make `sprint.metrics` (`internal/vault/sprint.go`, dispatch at `internal/vault/dispatch.go:179`) and `GET /api/v1/sprints/{id}/burndown` (`internal/server/metrics.go` / `sprints.go`) return the stored snapshot when the sprint carries one, without touching the git history walk, and set a provenance note saying the numbers were frozen at close. Only a sprint without a snapshot falls through to `BuildSprintMetrics` and the `HistorySource`.

## Acceptance Criteria

- [ ] A closed sprint with a snapshot answers from it and never invokes the history source (asserted with a fake that fails if called).
- [ ] The provenance note distinguishes "frozen at close" from live reconstruction.
- [ ] An open sprint behaves exactly as before.
- [ ] `go test -race ./internal/vault/... ./internal/server/...` covers both paths.
