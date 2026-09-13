---
id: GIT-T-0025
type: task
title: Store the YouTrack token in the companion config with an env override
status: done
priority: medium
parent: GIT-US-0048
milestone: GIT-M-0011
author: mcp
labels: [server, security, agent-ok]
estimate: 3
created: 2026-09-13T13:15:55Z
updated: 2026-09-13T14:41:30Z
started: 2026-09-13T14:41:18Z
closed: 2026-09-13T14:41:30Z
---

## Description

Add an `Integrations.YouTrack` section to `internal/config/config.go` mapping a gintrack project key to its token, loaded by `config.Load` (`internal/config/load.go:44`), saved by `config.Save` (`:74`) with the existing `0600` permissions, validated in `internal/config/validate.go:53`, and overridable by `GINTRACK_YOUTRACK_TOKEN` registered in `applyEnv` (`load.go:186`). Do not touch `GINTRACK_TOKEN`, which already means the companion bearer token and the go-git HTTP password. Add a `TokenSource` accessor returning `env`, `file` or `none` so surfaces can report provenance without exposing the value.

## Acceptance Criteria

- [x] The token loads and saves per project key, with the file written `0600`.
- [x] `GINTRACK_YOUTRACK_TOKEN` overrides the file and flag > env > file > default is covered by a test.
- [x] `TokenSource` reports the provenance and no code path returns or logs the token itself.
- [x] `go test -race ./internal/config/...` passes.

## Notes

`internal/config/youtrack.go`. `Config.Integrations.YouTrack` is `map[projectKey]YouTrackCredential`, tagged `json:"-"` so no `--json` output or API response can carry it; the flag and env override lives in an unexported field, so `Save` can never write it back to the file. `TokenSource` is `flag | env | file | none` — `flag` was added because the CLI needs it. `Config.YouTrackTokens()` hands the server a snapshot value with no exported field, no marshaler and a redacting `String()`.

`config.Validate` cannot reject "a token for an unknown project key": this package holds no inventory of project keys, which live in the repositories. It checks what it can — the key must look like a project key, and the token must not be empty — and a well-formed key naming no project simply never resolves a token.
