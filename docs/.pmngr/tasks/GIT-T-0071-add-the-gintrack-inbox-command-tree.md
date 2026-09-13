---
id: GIT-T-0071
type: task
title: Add the gintrack inbox command tree
status: done
priority: medium
parent: GIT-US-0066
milestone: GIT-M-0012
author: mcp
labels: [cli]
estimate: 4
created: 2026-09-13T13:17:03Z
updated: 2026-09-13T16:56:17Z
started: 2026-09-13T16:41:27Z
closed: 2026-09-13T16:56:17Z
---

## Description

Create `cmd/gintrack/inbox.go` with `newInboxCommand(flags *globalFlags)` and the subcommands `list`, `add`, `accept`, `reject` and `snooze`, registered in `cmd/gintrack/root.go:85-100`. Follow the shape of `cmd/gintrack/item.go:21-41`: the arg validators from `cmd/gintrack/exit.go:86-106`, `flags.resolve()` → `flags.printer(cmd, asJSON)` → `openVault(...)`, table output through `cmd/gintrack/output/output.go` with human notes on stderr, and pure formatters in `cmd/gintrack/format.go`. No business logic in the command file.

## Acceptance Criteria

- [x] All five subcommands work, support `--json` where a listing is produced, and use the shared exit codes (2 for a bad invocation).
- [x] `inbox list --status` filters and a snoozed item whose date has passed shows under `pending`.
- [x] `go test -race ./cmd/gintrack/...` covers the tree with the in-process harness (`harness_test.go:28`).
- [x] `docs/07-cli-and-api.md` §4 documents the command tree.

## Notes

Four of the five subcommands landed in an earlier wave: `list`, `accept`, `reject` and `snooze`, each `--json`-capable with human notes on stderr, each triaging through `inbox.triage` quoting the `rev` it read so a row somebody triaged first is a conflict rather than an overwrite.

**`inbox add` now exists**, and the two decisions it needed were settled by precedent rather than invented:

- **`--body -` reads standard input**, which is what `stdinMarker` and `readBody` (`cmd/gintrack/item.go:16,152`) already mean for `item new` and `item comment`. One convention for a piped body across the whole CLI.
- **An unqualified submission lands wherever the core says.** The command passes `project` only when `--project` was typed; `Vault.storeFor("")` already resolves the single-project case and refuses a multi-project workspace with `invalid_request` naming how many it found, which `vaultError` maps to exit 2. The command invents no project-picking rule of its own.

The command is deliberately thinner than `item new`: no `--status`, no `--parent`, no `--milestone`, because a submission has not been triaged yet and those are `accept`'s to choose — the same reason the MCP tool `create_inbox_item` is thin. It creates through `item.create` with an `inbox` block, so the CLI, the web app and MCP file submissions through one implementation. Default type `story`, default source `cli`.

Tests: `TestInboxAdd` (three shapes, each verified against the queue rather than against the answer), `TestInboxAddReadsAPipedBody`, `TestInboxAddRejectsABadInvocation` (five bad invocations, all exit 2) and `TestInboxAddWithoutATriageStatus`. `docs/07` §4.17 documents the flags, the defaults and both decisions.

`inbox.go` also holds the seam both this tree and `gintrack sprint` reach the core through — `openSpace` / `dispatch` / `vaultError` — which maps the vault error catalogue onto the documented exit codes.
