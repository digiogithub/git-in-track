---
id: GIT-US-0058
type: story
title: "CLI: gintrack youtrack connect and status"
status: backlog
priority: medium
parent: GIT-EP-0011
milestone: GIT-M-0011
author: mcp
labels: [cli, security, docs]
estimate: 3
created: 2026-09-13T13:12:27Z
updated: 2026-09-13T13:12:27Z
---

## Description

As someone setting up a machine or a CI checkout without opening a browser, I want `gintrack youtrack connect` and `gintrack youtrack status`, so that I can link a project and verify the credentials from a terminal or a provisioning script.

Add `cmd/gintrack/youtrack.go` with a cobra parent command and two subcommands, keeping the entry point thin as the repository layout demands — all real work goes to `internal/youtrack` and `internal/config`. `connect` takes `--url`, `--project`, `--project-key` (the gintrack project) and a token from `--token`, `$GINTRACK_YOUTRACK_TOKEN` or standard input when the flag is absent and stdin is not a TTY, validates it with `GET /api/users/me`, writes the token to the machine-local `0600` config and the `integrations.youtrack` block to `project.yaml`, then prints the resolved YouTrack user and project. `status` prints the configured URL, the gintrack-to-YouTrack project mapping, whether a token is present and where it came from, and, unless `--offline` is given, the result of a live probe. Neither command ever prints the token, and `--json` emits a machine-readable form for scripts.

## Acceptance Criteria

- [ ] `gintrack youtrack connect` and `gintrack youtrack status` exist, with help text in English and consistent with the other commands.
- [ ] The token is read from `--token`, then `$GINTRACK_YOUTRACK_TOKEN`, then stdin, matching the documented flag > env > file precedence.
- [ ] `connect` fails without writing anything when the probe returns 401, 403 or 404, printing which of the three it was.
- [ ] Neither command prints or logs the token, in either text or `--json` output; a test asserts this.
- [ ] `connect` writes `project.yaml` surgically, leaving every other key and comment intact.
- [ ] `docs/07-cli-and-api.md` documents both commands, their flags and their exit codes.
- [ ] `go test -race ./cmd/...` covers argument parsing, the token source precedence and the redaction assertion.

## Notes

Existing commands under `cmd/gintrack/` are the style reference; the config helpers are `config.Load` (`internal/config/load.go:44`), `config.Save` (`:74`) and `config.Resolve` (`:146`). The YouTrack probe is `Client.Me` from the client story.

Do NOT add an interactive prompt loop or a TTY password reader library — reading stdin when it is piped is enough and keeps the command scriptable. Do NOT let this command create or migrate a project: it only writes the integration block into an existing `project.yaml`.
