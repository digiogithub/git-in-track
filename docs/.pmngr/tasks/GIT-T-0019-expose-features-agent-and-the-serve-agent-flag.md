---
id: GIT-T-0019
type: task
title: Expose features.agent and the serve --agent flag
status: done
priority: medium
parent: GIT-US-0049
milestone: GIT-M-0013
author: mcp
labels: [server, cli, agent-ok]
estimate: 2
created: 2026-09-13T13:15:44Z
updated: 2026-09-15T16:43:05Z
started: 2026-09-15T15:28:32Z
closed: 2026-09-15T16:43:05Z
---

## Description

Add `"agent"` to the `features` map in `handleCapabilities` (`internal/server/server.go:411-435`), true only when a Pando URL is configured and the feature was enabled. Add `--agent` to `cmd/gintrack/serve.go` next to `--mcp-http`, plumb it through `serveFlags`, `resolveServeConfig` and `server.Options`, and print a startup line naming the Pando URL the way the MCP and tunnel features do. On the web side, read it in `toCapabilities` (`web/src/api/companion-provider.ts:751-766`), add it to the `Capabilities` type and defaults in `web/src/api/provider.ts`, and report false from `browser-provider.ts`.

## Acceptance Criteria

- [ ] `features.agent` is false without the flag or without a URL, true with both.
- [ ] `gintrack serve --agent` starts and logs the Pando target; `--help` documents the flag.
- [ ] All four providers compile against the widened `Capabilities` type, with Vitest covering the mapping.
