---
id: GIT-T-0222
type: task
title: Add the gintrack youtrack kb push and pull commands
status: done
priority: medium
parent: GIT-US-0094
milestone: GIT-M-0011
author: mcp
labels: [cli, agent-ok]
estimate: 3
created: 2026-09-13T13:21:54Z
updated: 2026-09-13T16:39:49Z
started: 2026-09-13T16:39:11Z
closed: 2026-09-13T16:39:49Z
---

## Description

Add a `kb` subcommand group under the `youtrack` command in `cmd/gintrack/youtrack.go` with `push <path>` and `pull <path>`, each taking `--recursive`, `--wait` and `--json`. Without `--wait` the command prints the job id; with `--wait` it follows the job and prints a table of page, action and article id, and exits non-zero when any page failed or conflicted so a CI step can fail on it.

## Acceptance Criteria

- [x] Both subcommands work with all three flags and validate args through `cmd/gintrack/exit.go`.
- [x] `--wait` returns a non-zero exit code on any failure or conflict.
- [x] `--json` emits the raw result with human lines on stderr.
- [x] `go test -race ./cmd/gintrack/...` covers both commands through the in-process harness.

## Notes

Landed in `cmd/gintrack/youtracksync.go`, with a third subcommand `kb status [path]` that keeps `--remote` behind an explicit flag as the task comment asked — it is one request per page.

`--wait` follows the job and then re-reads `youtrack.kb.status` for the same selection rather than trusting the queueing answer: the pages moved on disk, and a conflict the job wrote is recorded locally. The two failure modes are given **different** exit codes so a CI step can tell them apart: `5` when any page conflicted (the page is untouched, `<page>.conflict.md` holds the other side, nobody has reconciled them) and `1` when the job itself failed. `--json` prints a declared `kbWaitPayload` carrying `ok`, `state`, `conflicts` and the per-page states, so a script branches on a documented shape rather than on the engine's job record.
