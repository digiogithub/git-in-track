---
id: GIT-T-0195
type: task
title: Add the gintrack youtrack push-comments command
status: done
priority: medium
parent: GIT-US-0079
milestone: GIT-M-0011
author: mcp
labels: [cli, agent-ok]
estimate: 2
created: 2026-09-13T13:20:06Z
updated: 2026-09-13T16:39:37Z
started: 2026-09-13T16:39:10Z
closed: 2026-09-13T16:39:37Z
---

## Description

Add `push-comments <item>` under the `youtrack` command in `cmd/gintrack/youtrack.go` with `--all`, `--comment <path>`, `--wait` and `--json`. Without `--wait` it prints the job id; with `--wait` it follows the job and prints a table of comment, remote id and result. Exit codes and arg validation come from `cmd/gintrack/exit.go`.

## Acceptance Criteria

- [x] The subcommand works with all four flags and validates args through the shared helpers.
- [x] `--wait` prints the result table and returns a non-zero exit code when any comment failed.
- [x] `--json` emits the raw result with human lines on stderr.
- [x] `go test -race ./cmd/gintrack/...` covers it through the in-process harness.

## Notes

Landed in `cmd/gintrack/youtracksync.go`. The command opens a companion with no listener — the same `internal/server` wiring `gintrack serve` builds — starts the background engine and dispatches `youtrack.comment.push`; `--wait` follows the job through `server.Headless.WaitForJob` and then re-reads the push result, so the remote ids it prints are the ones the job actually wrote back rather than a reprint of the queueing answer. Interrupting `--wait` stops the watching and not the job, which is what the message says.

An item with no YouTrack reference is refused by the vault with `invalid_request` and mapped to exit 3, the validation/precondition code, not a generic failure.
