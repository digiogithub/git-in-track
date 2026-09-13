---
id: GIT-US-0094
type: story
title: MCP tools and CLI commands for KB sync
status: in_review
priority: medium
parent: GIT-EP-0014
milestone: GIT-M-0011
author: mcp
labels: [mcp, cli, docs]
estimate: 3
created: 2026-09-13T13:15:31Z
updated: 2026-09-13T15:41:43Z
started: 2026-09-13T15:41:34Z
---

## Description

As an agent or a release engineer, I want `publish_kb_page_to_youtrack` and `sync_kb_page_from_youtrack` over MCP and `gintrack youtrack kb push|pull` on the command line, so that documentation can be published from a script or a CI step.

Add both tools to `internal/mcp/tools_youtrack.go` as write tools with `In` `{project, path, recursive}`, returning `{jobId, pages: [{path, action, articleId, url, error?}]}`. Every path argument goes through `s.guard.Check` (`internal/mcp/paths.go:122`) before it reaches the vault, since a KB path is user-supplied and the guard is what keeps it inside the roots. The handlers dispatch to the KB sync vault operations and announce the write; the surface-pinning tests in `internal/mcp/tools_test.go` and `cmd/gintrack/mcp_test.go` must be updated in the same change.

Add `kb push <path>` and `kb pull <path>` under the `youtrack` CLI command, with `--recursive`, `--wait` and `--json`. Without `--wait` the command enqueues and prints the job id; with `--wait` it follows the job and prints a table of page, action and article id, and exits non-zero when any page failed or produced a conflict so a CI step notices.

## Acceptance Criteria

- [ ] Both MCP tools are registered as write tools, absent on a read-only server, with path arguments checked by the path guard.
- [ ] MCP and CLI surface-pinning tests are updated and pass.
- [ ] `gintrack youtrack kb push|pull <path>` supports `--recursive`, `--wait` and `--json`.
- [ ] With `--wait`, a failure or a conflict on any page produces a non-zero exit code from `cmd/gintrack/exit.go`.
- [ ] `docs/08-mcp-server.md` §4, `docs/07-cli-and-api.md` §4 and `CHANGELOG.md` are updated.
- [ ] `go test -race ./internal/mcp/... ./cmd/gintrack/...` passes, including a behaviour test through the in-memory MCP client harness.

## Notes

Depends on the vault and REST operations of this epic and on GIT-EP-0012 for `tools_youtrack.go` and the `youtrack` CLI parent command — create them once and extend, do not fork.

Checklists: MCP tool addition is the scratchpad gintrack report §5.5, CLI command addition §6. The path guard is `internal/mcp/paths.go` (`cleanVaultPath` :27, `PathGuard.Check` :122).

Do NOT implement sync logic in the tool or command layers. Do NOT let a conflict look like success on the CLI; CI has to be able to fail on it.
