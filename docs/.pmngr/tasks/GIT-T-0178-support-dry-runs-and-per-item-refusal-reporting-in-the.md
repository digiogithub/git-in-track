---
id: GIT-T-0178
type: task
title: Support dry runs and per-item refusal reporting in the sprint CLI
status: done
priority: medium
parent: GIT-US-0092
milestone: GIT-M-0012
author: mcp
labels: [cli, agent-ok]
estimate: 2
created: 2026-09-13T13:19:36Z
updated: 2026-09-13T16:42:15Z
started: 2026-09-13T16:41:35Z
closed: 2026-09-13T16:42:15Z
---

## Description

Add `--dry-run` to `gintrack sprint close` and `gintrack sprint transfer`, printing the close report — per-outcome counts, the target and each refusal on its own line — and writing nothing, which is what a CI job asking "is this sprint clean?" needs. Without the flag, print what actually changed. Set a non-zero exit code only when nothing at all could be applied, so a single `repo_not_cloned` does not fail a whole rollover.

## Acceptance Criteria

- [x] `--dry-run` prints the report, writes nothing and exits 0.
- [x] Per-item refusals are printed on their own lines in both modes.
- [x] A run where nothing could be applied exits non-zero; a partially applied one exits 0 with warnings on stderr.
- [x] `go test -race ./cmd/gintrack/...` covers all three outcomes.

## Notes

Refusals print as `refused <ref>: <reason>`. A partial run exits 0 with `N of M decisions were refused` on stderr; only a run where every decision was refused exits 1. The dry-run tests assert the sprint and item files are byte-identical afterwards rather than merely that no error was reported.
