---
id: GIT-T-0073
type: task
title: "Implement connect: token sources, probe and config write"
status: todo
priority: medium
parent: GIT-US-0058
milestone: GIT-M-0011
author: mcp
labels: [cli, security, agent-ok]
estimate: 3
created: 2026-09-13T13:17:07Z
updated: 2026-09-13T13:17:07Z
---

## Description

Implement `connect`: resolve the token from `--token`, then `$GINTRACK_YOUTRACK_TOKEN`, then standard input when the flag is absent and stdin is piped; validate it with `youtrack.Client.Me`; on success write the token to the machine-local `0600` config and the `integrations.youtrack` block to `project.yaml` through the surgical writer; print the resolved YouTrack user and project. On a 401, 403 or 404 write nothing and say which it was.

## Acceptance Criteria

- [ ] Token precedence flag > env > stdin is covered by a test.
- [ ] A failed probe leaves both the config file and `project.yaml` untouched.
- [ ] Neither the text nor the `--json` output contains the token, asserted by a test.
- [ ] `go test -race ./cmd/...` passes against an `httptest` YouTrack stub.
