---
id: GIT-T-0040
type: task
title: Implement GET and PATCH /api/v1/youtrack/settings
status: done
priority: medium
parent: GIT-US-0052
milestone: GIT-M-0011
author: mcp
labels: [server, security, agent-ok]
estimate: 3
created: 2026-09-13T13:16:17Z
updated: 2026-09-13T14:42:39Z
started: 2026-09-13T14:42:17Z
closed: 2026-09-13T14:42:39Z
---

## Description

Implement the settings pair: `GET` returns `{url, project, fieldMap, pushComments, kbSync, hasToken, tokenSource, persisted}` and never the token; `PATCH` accepts a sparse body, writes the committed half to `project.yaml` through the surgical writer and the token to the machine-local config, then answers with `persisted bool`. Copy `handleGitSettingsPatch` (`internal/server/git.go:374-393`) and `gitState.persist` (`:336-352`) exactly, including returning `persisted: false` when `Options.ConfigPath` (`server.go:112`) is empty.

## Acceptance Criteria

- [x] `GET` never returns the token and reports `hasToken` and `tokenSource` (`env`, `file` or `none`).
- [x] `PATCH` applies a sparse body, validates the URL and project, and reports `persisted` correctly with and without a config path.
- [x] `go test -race ./internal/server/...` covers both handlers including the redaction assertion.

## Notes

`GET|PATCH /api/v1/youtrack/settings?key=<projectKey>`. `key` is the git-in-track project key and may be omitted when the companion serves exactly one project. `tokenSource` has a fourth value, `flag`, because the CLI can supply a token for one process.

Every field of the patch is a pointer, so "absent" and "set to empty" stay distinguishable — which is what makes disconnecting a project expressible at all. A patch that would not load back is refused with `400 invalid_request` before anything is written, so `project.yaml` is never left half-edited. `persisted` reports the token half only: the `project.yaml` half is written to a file by definition.
