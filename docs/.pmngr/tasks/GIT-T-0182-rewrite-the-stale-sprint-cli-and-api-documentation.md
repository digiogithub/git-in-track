---
id: GIT-T-0182
type: task
title: Rewrite the stale sprint CLI and API documentation
status: done
priority: medium
parent: GIT-US-0092
milestone: GIT-M-0012
author: mcp
labels: [docs, agent-ok]
estimate: 2
created: 2026-09-13T13:19:41Z
updated: 2026-09-13T17:24:00Z
started: 2026-09-13T17:23:40Z
closed: 2026-09-13T17:24:00Z
---

## Description

Replace the stale specification at `docs/07-cli-and-api.md` §4.6 (line 731), which describes `board`, `sprint` and `retro` commands that do not exist, with the sprint command tree as built, and document the extended `POST /api/v1/sprints/{id}/close` and the new `POST /api/v1/sprints/{id}/transfer` plus the `sprint.changed` WS topic in §5.5. Note explicitly that `board` and `retro` commands are still not implemented rather than leaving them described as if they were.

## Acceptance Criteria

- [x] §4.6 documents only what exists, with the flags and exit codes, and says what does not.
- [x] §5.5 documents both endpoints with their request and response shapes and error codes, and the WS topic is in the contract section.
- [x] `make lint` passes.

## Notes

The second criterion was already satisfied and was verified against the code: §5.5 documents `POST /sprints/{id}/close` with the bulk `transfer` decision, the per-item `carry` override, `dryRun` publishing no event at all, and `POST /sprints/{id}/transfer` with a worked request and response, `If-Match`, `412` on a stale revision, the per-item `repo_not_cloned` line that is a `200` rather than a failure, and `sprint_target_completed` (409). `sprint.changed` is in the §5.6 contract section with its payload and the note that a dry run publishes neither it nor `item.changed`. The sprint tree itself was moved to its own §4.16 in an earlier wave and documents the five subcommands, their flags, the derived status they list by and the exit-code rule (`0` when anything could be applied, `1` only when nothing could).

The first criterion was **not** satisfied and is the work this task needed. §4.6 still opened "Team-repository boards (Phase 3)" and showed worked `gintrack board get` / `board move` output as if the command existed, with a confusing "*As built.* Only the board subcommands above are planned" line underneath. Checked against `cmd/gintrack/root.go`: the registered commands are serve, mcp, version, completion, init, add, ls, rm, index, snapshot, sync, item, inbox, sprint, doctor, config and youtrack — there is no `board` command and no `retro` command, and no `board.go` or `retro.go` in `cmd/gintrack/` at all.

§4.6 is now titled "`gintrack board …` and `gintrack retro …` — **not implemented**" and says so in its first sentence, points a reader at the two routes that do work today (REST `/api/v1/boards` and `/api/v1/retros`, and the Markdown files in the team repository), keeps the planned tree clearly labelled as planned, and drops the fabricated terminal transcripts. The remote-card behaviour a future implementation has to reproduce is kept as prose.
