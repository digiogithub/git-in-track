---
id: GIT-T-0076
type: task
title: Implement status with an optional live probe
status: todo
priority: medium
parent: GIT-US-0058
milestone: GIT-M-0011
author: mcp
labels: [cli, agent-ok]
estimate: 2
created: 2026-09-13T13:17:11Z
updated: 2026-09-13T13:17:11Z
---

## Description

Implement `status`: print the configured instance URL, the gintrack-to-YouTrack project mapping, whether a token is present and its source (`env` or `file`), and, unless `--offline` is given, the result of a live `Me` probe. Exit non-zero when the integration is not configured or the probe fails, so a provisioning script can branch on it.

## Acceptance Criteria

- [ ] Output covers URL, project mapping, token presence and source, and probe result.
- [ ] `--offline` skips all network access; exit codes distinguish not-configured from probe failure.
- [ ] `--json` emits the same information in a stable machine-readable shape, without the token.
- [ ] `go test -race ./cmd/...` covers configured, unconfigured and probe-failure cases.
