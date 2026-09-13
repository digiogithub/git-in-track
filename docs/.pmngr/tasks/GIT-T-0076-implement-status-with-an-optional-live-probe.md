---
id: GIT-T-0076
type: task
title: Implement status with an optional live probe
status: done
priority: medium
parent: GIT-US-0058
milestone: GIT-M-0011
author: mcp
labels: [cli, agent-ok]
estimate: 2
created: 2026-09-13T13:17:11Z
updated: 2026-09-13T14:43:31Z
started: 2026-09-13T14:43:09Z
closed: 2026-09-13T14:43:31Z
---

## Description

Implement `status`: print the configured instance URL, the gintrack-to-YouTrack project mapping, whether a token is present and its source (`env` or `file`), and, unless `--offline` is given, the result of a live `Me` probe. Exit non-zero when the integration is not configured or the probe fails, so a provisioning script can branch on it.

## Acceptance Criteria

- [x] Output covers URL, project mapping, token presence and source, and probe result.
- [x] `--offline` skips all network access; exit codes distinguish not-configured from probe failure.
- [x] `--json` emits the same information in a stable machine-readable shape, without the token.
- [x] `go test -race ./cmd/...` covers configured, unconfigured and probe-failure cases.

## Notes

Exit 4 for not configured (no block, or no token), exit 1 for a failed probe with a message saying whether it was a 401, a 403 or a 404, exit 0 otherwise. `--offline` reports the configuration and sets `probed: false`; a test closes the stub instance first, so a probe that happened anyway would fail the test rather than pass silently. Output is printed before the exit code is decided, so a script gets both.
