---
id: GIT-T-0214
type: task
title: Expose the KB sync REST routes
status: todo
priority: medium
parent: GIT-US-0090
milestone: GIT-M-0011
author: mcp
labels: [server, agent-ok]
estimate: 3
created: 2026-09-13T13:21:14Z
updated: 2026-09-13T13:21:14Z
---

## Description

Add `GET /api/v1/youtrack/kb/status?path=&recursive=`, `POST /api/v1/youtrack/kb/publish` and `POST /api/v1/youtrack/kb/pull` beside the KB routes (`internal/server/kb.go:19-23`), mounted through the same per-project and per-team mounts as `/kb` (`internal/server/api.go:53`, `:58`, `:70`) and gated on `features.youtrack`. Handlers stay thin: parse, then `s.call(...)` into the vault as `internal/server/api.go:134-156` does.

## Acceptance Criteria

- [ ] All three routes exist, are mounted per project and per team, and are gated on the capability.
- [ ] An unlinked project returns 404 and the handlers contain no sync logic.
- [ ] `go test -race ./internal/server/...` covers each route including the gating.
