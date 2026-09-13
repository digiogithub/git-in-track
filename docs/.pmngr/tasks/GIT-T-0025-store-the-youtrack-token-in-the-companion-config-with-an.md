---
id: GIT-T-0025
type: task
title: Store the YouTrack token in the companion config with an env override
status: todo
priority: medium
parent: GIT-US-0048
milestone: GIT-M-0011
author: mcp
labels: [server, security, agent-ok]
estimate: 3
created: 2026-09-13T13:15:55Z
updated: 2026-09-13T13:15:55Z
---

## Description

Add an `Integrations.YouTrack` section to `internal/config/config.go` mapping a gintrack project key to its token, loaded by `config.Load` (`internal/config/load.go:44`), saved by `config.Save` (`:74`) with the existing `0600` permissions, validated in `internal/config/validate.go:53`, and overridable by `GINTRACK_YOUTRACK_TOKEN` registered in `applyEnv` (`load.go:186`). Do not touch `GINTRACK_TOKEN`, which already means the companion bearer token and the go-git HTTP password. Add a `TokenSource` accessor returning `env`, `file` or `none` so surfaces can report provenance without exposing the value.

## Acceptance Criteria

- [ ] The token loads and saves per project key, with the file written `0600`.
- [ ] `GINTRACK_YOUTRACK_TOKEN` overrides the file and flag > env > file > default is covered by a test.
- [ ] `TokenSource` reports the provenance and no code path returns or logs the token itself.
- [ ] `go test -race ./internal/config/...` passes.
