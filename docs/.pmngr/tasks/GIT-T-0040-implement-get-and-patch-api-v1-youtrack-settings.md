---
id: GIT-T-0040
type: task
title: Implement GET and PATCH /api/v1/youtrack/settings
status: todo
priority: medium
parent: GIT-US-0052
milestone: GIT-M-0011
author: mcp
labels: [server, security, agent-ok]
estimate: 3
created: 2026-09-13T13:16:17Z
updated: 2026-09-13T13:16:17Z
---

## Description

Implement the settings pair: `GET` returns `{url, project, fieldMap, pushComments, kbSync, hasToken, tokenSource, persisted}` and never the token; `PATCH` accepts a sparse body, writes the committed half to `project.yaml` through the surgical writer and the token to the machine-local config, then answers with `persisted bool`. Copy `handleGitSettingsPatch` (`internal/server/git.go:374-393`) and `gitState.persist` (`:336-352`) exactly, including returning `persisted: false` when `Options.ConfigPath` (`server.go:112`) is empty.

## Acceptance Criteria

- [ ] `GET` never returns the token and reports `hasToken` and `tokenSource` (`env`, `file` or `none`).
- [ ] `PATCH` applies a sparse body, validates the URL and project, and reports `persisted` correctly with and without a config path.
- [ ] `go test -race ./internal/server/...` covers both handlers including the redaction assertion.
