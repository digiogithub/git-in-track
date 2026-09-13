---
id: GIT-T-0028
type: task
title: Report features.youtrack in the capabilities endpoint
status: todo
priority: medium
parent: GIT-US-0048
milestone: GIT-M-0011
author: mcp
labels: [server, agent-ok]
estimate: 1
created: 2026-09-13T13:15:58Z
updated: 2026-09-13T13:15:58Z
---

## Description

Add `features.youtrack` to `handleCapabilities` (`internal/server/server.go:411-435`), true only in companion mode when an integration block is configured for at least one mounted project. Browser-only mode never reports it, matching how `git`, `ssh`, `watcher` and `mcpHttp` already behave.

## Acceptance Criteria

- [ ] `GET /api/v1/capabilities` reports `features.youtrack` with the documented semantics.
- [ ] `go test -race ./internal/server/...` covers both the configured and unconfigured cases.
- [ ] `docs/07-cli-and-api.md` lists the new capability flag.
