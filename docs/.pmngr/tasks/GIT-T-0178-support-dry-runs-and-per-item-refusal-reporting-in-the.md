---
id: GIT-T-0178
type: task
title: Support dry runs and per-item refusal reporting in the sprint CLI
status: todo
priority: medium
parent: GIT-US-0092
milestone: GIT-M-0012
author: mcp
labels: [cli, agent-ok]
estimate: 2
created: 2026-09-13T13:19:36Z
updated: 2026-09-13T13:19:36Z
---

## Description

Add `--dry-run` to `gintrack sprint close` and `gintrack sprint transfer`, printing the close report — per-outcome counts, the target and each refusal on its own line — and writing nothing, which is what a CI job asking "is this sprint clean?" needs. Without the flag, print what actually changed. Set a non-zero exit code only when nothing at all could be applied, so a single `repo_not_cloned` does not fail a whole rollover.

## Acceptance Criteria

- [ ] `--dry-run` prints the report, writes nothing and exits 0.
- [ ] Per-item refusals are printed on their own lines in both modes.
- [ ] A run where nothing could be applied exits non-zero; a partially applied one exits 0 with warnings on stderr.
- [ ] `go test -race ./cmd/gintrack/...` covers all three outcomes.
