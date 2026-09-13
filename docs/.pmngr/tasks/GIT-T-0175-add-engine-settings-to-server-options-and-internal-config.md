---
id: GIT-T-0175
type: task
title: Add engine settings to server.Options and internal/config
status: todo
priority: medium
parent: GIT-US-0084
milestone: GIT-M-0011
author: mcp
labels: [server, agent-ok]
estimate: 2
created: 2026-09-13T13:19:31Z
updated: 2026-09-13T13:19:31Z
---

## Description

Add a `SyncEngine` configuration struct to `internal/config/config.go` with workers, batch size, rate limit, max attempts and journal retention, validated in `internal/config/validate.go:53`, and surface it on `server.Options` (`internal/server/server.go:59-131`) next to `Git config.Git` and `Tunnel config.Tunnel`. Defaults are 2 workers, batch 20, 5 req/s, 5 attempts and 7 days.

## Acceptance Criteria

- [ ] The struct exists in both places with the documented defaults and range validation.
- [ ] An out-of-range value fails config validation with a message naming the key.
- [ ] `go test -race ./internal/config/... ./internal/server/...` passes.
