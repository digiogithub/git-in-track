---
id: GIT-T-0009
type: task
title: Add agent.pando configuration and validation
status: done
priority: medium
parent: GIT-US-0049
milestone: GIT-M-0013
author: mcp
labels: [server, agent-ok]
estimate: 2
created: 2026-09-13T13:15:24Z
updated: 2026-09-15T16:42:54Z
started: 2026-09-15T15:28:29Z
closed: 2026-09-15T16:42:54Z
---

## Description

Add an `Agent` struct with a nested `Pando` section to `internal/config/config.go`, holding `url`, `token`, `agent` (default `coder`) and `insecure_tls`, with the same JSON and YAML tags as the neighbouring `Git`, `MCP` and `Tunnel` sections. Wire it into `config.Resolve` and `applyEnv` (`internal/config/load.go:146`, `:186`) so `GINTRACK_PANDO_TOKEN` overrides the file value, keeping the documented precedence flag > env > file > default. Add validation in `internal/config/validate.go`: an empty URL disables the feature, a malformed URL or an unsupported scheme is an error.

Do not reuse `GINTRACK_TOKEN` — it already means both the companion bearer token and the go-git HTTP fallback password.

## Acceptance Criteria

- [ ] The section round-trips through `config.Parse` and `config.Save` with no key reordering.
- [ ] `GINTRACK_PANDO_TOKEN` overrides the file token and nothing else.
- [ ] `go test -race ./internal/config/...` covers parsing, the env override, the default agent name and both validation failures.
