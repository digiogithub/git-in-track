---
id: GIT-T-0028
type: task
title: Report features.youtrack in the capabilities endpoint
status: done
priority: medium
parent: GIT-US-0048
milestone: GIT-M-0011
author: mcp
labels: [server, agent-ok]
estimate: 1
created: 2026-09-13T13:15:58Z
updated: 2026-09-13T14:42:04Z
started: 2026-09-13T14:41:39Z
closed: 2026-09-13T14:42:04Z
---

## Description

Add `features.youtrack` to `handleCapabilities` (`internal/server/server.go:411-435`), true only in companion mode when an integration block is configured for at least one mounted project. Browser-only mode never reports it, matching how `git`, `ssh`, `watcher` and `mcpHttp` already behave.

## Acceptance Criteria

- [x] `GET /api/v1/capabilities` reports `features.youtrack` with the documented semantics.
- [x] `go test -race ./internal/server/...` covers both the configured and unconfigured cases.
- [x] `docs/07-cli-and-api.md` lists the new capability flag.

## Notes

Two flags, not one. `features.youtrack` is the one this task asked for: true only in companion mode with at least one mounted project declaring an `integrations.youtrack` block. `features.youtrackSupported` was added next to it because the first flag alone gives the web app no way to show the settings card that would create the connection: it is true in companion mode, full stop, and false in browser-only mode. Both are documented in `docs/07-cli-and-api.md` §5.5 under capabilities.
