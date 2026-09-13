---
id: GIT-US-0079
type: story
title: MCP tool and CLI command for pushing comments
status: done
priority: medium
parent: GIT-EP-0013
milestone: GIT-M-0011
author: mcp
labels: [mcp, cli, docs]
estimate: 3
created: 2026-09-13T13:14:09Z
updated: 2026-09-13T16:40:56Z
started: 2026-09-13T15:41:32Z
closed: 2026-09-13T16:40:56Z
---

## Description

As an agent reporting progress, I want `push_comment_to_youtrack` over MCP and `gintrack youtrack push-comments <item>` on the command line, so that a comment written by a script or an agent can reach the issue tracker without the web UI.

Add the tool to `internal/mcp/tools_youtrack.go` (created by GIT-EP-0012) as a write tool with `In` `{project, itemId, commentPath?, all?}` — `commentPath` pushes one comment, `all: true` pushes every not-yet-pushed comment of the item — returning `{pushed: [{commentPath, youtrackCommentId, url}], skipped: [...], failed: [{commentPath, error}]}`. The handler validates with `invalidField`, guards any path with `s.guard.Check`, dispatches to the vault operation, and announces the write. The surface-pinning tests in `internal/mcp/tools_test.go` and `cmd/gintrack/mcp_test.go` must be updated.

Add the `push-comments <item>` subcommand under the `youtrack` command in `cmd/gintrack/youtrack.go`, with `--all`, `--comment <path>`, `--wait` and `--json`. Without `--wait` the command enqueues and prints the job id; with `--wait` it follows the job to completion and prints a table of comment, remote id and result. Exit codes come from `cmd/gintrack/exit.go` and the arg validators there, not from cobra's raw ones.

## Acceptance Criteria

- [ ] `push_comment_to_youtrack` is registered as a write tool, absent on a read-only server, and returns pushed, skipped and failed lists.
- [ ] MCP and CLI surface-pinning tests are updated and pass.
- [ ] `gintrack youtrack push-comments <item>` supports `--all`, `--comment`, `--wait` and `--json`; without `--wait` it prints the job id.
- [ ] An item with no YouTrack `external` reference produces a clear non-retryable error on both surfaces.
- [ ] `docs/08-mcp-server.md` §4, `docs/07-cli-and-api.md` §4 and `CHANGELOG.md` are updated.
- [ ] `go test -race ./internal/mcp/... ./cmd/gintrack/...` passes, including a behaviour test through the in-memory MCP client harness.

## Notes

Depends on the push job kind of this epic and on GIT-EP-0012 for `tools_youtrack.go` and the `youtrack` CLI parent command — coordinate so the file and the command are created once, not twice.

Checklists: MCP tool addition is the scratchpad gintrack report §5.5, CLI command addition §6. Human-readable lines must go to stderr in JSON mode (`cmd/gintrack/output/output.go:126`).

Do NOT put push logic in `internal/mcp` or in the cobra command — both only validate, dispatch and format.
