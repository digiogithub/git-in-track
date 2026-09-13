---
id: GIT-US-0092
type: story
title: Sprint CLI commands and the cycles documentation pass
status: done
priority: medium
parent: GIT-EP-0017
milestone: GIT-M-0012
author: mcp
labels: [cli, docs]
estimate: 5
created: 2026-09-13T13:15:18Z
updated: 2026-09-13T17:26:52Z
started: 2026-09-13T17:26:29Z
closed: 2026-09-13T17:26:52Z
---

## Description

As someone running the release train from a terminal or a CI job, I want `gintrack sprint` to list, start, close and transfer, so that the cadence can be scripted and the documented CLI surface finally matches the product.

There is no `sprint` command today even though `docs/07-cli-and-api.md` §4.6 specifies one. This story adds `cmd/gintrack/sprint.go` with `newSprintCommand(flags *globalFlags)` and the subcommands `list` (grouped by derived status, `--status` filter, `--board`, `--json`), `show <id>`, `start <id> [--force]`, `close <id> [--transfer next|backlog|none] [--target <SPRINT-ID>] [--dry-run]` and `transfer <id> --to <SPRINT-ID> [--dry-run]`. It is built exactly like the existing commands: the arg validators from `cmd/gintrack/exit.go:86-106` so a bad invocation exits 2, `flags.resolve()` → `flags.printer(cmd, asJSON)` → `openVault(...)`, human lines on stderr so `… --json | jq` stays safe (`cmd/gintrack/output/output.go:126`), pure formatting in `cmd/gintrack/format.go`, registration in `cmd/gintrack/root.go:85-100`, and tests through the in-process harness at `cmd/gintrack/harness_test.go:28`. `--dry-run` prints the close report and writes nothing, which is what a CI job checking "is the sprint clean?" wants.

The documentation half finishes the epic: `docs/04-team-repository.md` §8 gains the derived-status table, the draft rules and the snapshot block; `docs/07-cli-and-api.md` §4.6 and §5.5 gain the command tree, the extended close endpoint and the transfer endpoint; `docs/08-mcp-server.md` §4 gains `close_sprint` and `transfer_sprint_items`; ADR-034 is linked from `docs/adr/README.md`; and `CHANGELOG.md` records optional dates, derived status, the snapshot and the transfer under Unreleased.

## Acceptance Criteria

- [x] `gintrack sprint list|show|start|close|transfer` exists, is registered in `root.go`, and uses the shared exit codes and arg validators.
- [x] `sprint list` groups by derived status and supports `--status`, `--board` and `--json`; JSON output carries the derived status and the snapshot when present.
- [x] `sprint close --dry-run` and `sprint transfer --dry-run` print the report and write nothing; without the flag they apply and print what changed.
- [x] Per-item refusals (`repo_not_cloned` and friends) are printed on their own lines and set a non-zero exit code only when nothing could be applied.
- [x] `docs/07-cli-and-api.md` §4.6 and §5.5 document the commands and the endpoints, replacing the stale specification.
- [x] `docs/04-team-repository.md` §8 documents optional dates, the derived-status table, the draft overlap exemption and the `snapshot` block.
- [x] `docs/08-mcp-server.md` §4 lists the two new tools and `CHANGELOG.md` records the whole epic under Unreleased.
- [x] `go test -race ./cmd/gintrack/...` covers each subcommand including the dry runs; `make lint` passes.

## Notes

Existing code: `cmd/gintrack/root.go:50-100`, `cmd/gintrack/item.go:21-41` (the closest command tree), `cmd/gintrack/workspace.go:78-173`, `cmd/gintrack/output/output.go:27-162`, `cmd/gintrack/format.go:13-82`, `cmd/gintrack/exit.go:14-106`, `cmd/gintrack/harness_test.go:28`. The stale CLI specification is `docs/07-cli-and-api.md:731` (§4.6) — it describes `board`/`sprint`/`retro` commands that do not exist.

Conventional commit scopes are one per commit: split this story's work into a `feat(cli): …` commit and a `docs: …` commit rather than one mixed change.

Do NOT put business logic in the command files (`cmd/gintrack/main.go:5-7`) — they resolve config, open the vault, call `Dispatch` and print. Do NOT add a `retro` or `board` command here; this story is scoped to sprints.

### One numbering deviation

The fifth criterion says the command tree lands in §4.6. It landed in **§4.16** instead, because §4.6 is where the `board` and `retro` commands were specified and those commands still do not exist — turning that section into the sprint tree would have deleted the only record that they are unbuilt. §4.6 now names itself "not implemented", says so in its first sentence, keeps the planned board/retro tree clearly labelled as planned, and points at §4.16 for the sprint commands that are real. The stale specification the criterion cares about is gone either way.
