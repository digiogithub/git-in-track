---
id: GIT-T-0071
type: task
title: Add the gintrack inbox command tree
status: in_review
priority: medium
parent: GIT-US-0066
milestone: GIT-M-0012
author: mcp
labels: [cli]
estimate: 4
created: 2026-09-13T13:17:03Z
updated: 2026-09-13T16:41:48Z
started: 2026-09-13T16:41:27Z
---

## Description

Create `cmd/gintrack/inbox.go` with `newInboxCommand(flags *globalFlags)` and the subcommands `list`, `add`, `accept`, `reject` and `snooze`, registered in `cmd/gintrack/root.go:85-100`. Follow the shape of `cmd/gintrack/item.go:21-41`: the arg validators from `cmd/gintrack/exit.go:86-106`, `flags.resolve()` → `flags.printer(cmd, asJSON)` → `openVault(...)`, table output through `cmd/gintrack/output/output.go` with human notes on stderr, and pure formatters in `cmd/gintrack/format.go`. No business logic in the command file.

## Acceptance Criteria

- [ ] All five subcommands work, support `--json` where a listing is produced, and use the shared exit codes (2 for a bad invocation).
- [x] `inbox list --status` filters and a snoozed item whose date has passed shows under `pending`.
- [x] `go test -race ./cmd/gintrack/...` covers the tree with the in-process harness (`harness_test.go:28`).
- [x] `docs/07-cli-and-api.md` §4 documents the command tree.

## Notes

Four of the five subcommands landed in `cmd/gintrack/inbox.go`: `list`, `accept`, `reject` and `snooze`, each `--json`-capable with human notes on stderr, each triaging through `inbox.triage` quoting the `rev` it read so a row somebody triaged first is a conflict rather than an overwrite. `docs/07` §4.17 documents them.

**`inbox add` is not written.** It was out of the scope this wave was given, and it is the one subcommand that needs a decision rather than a dispatch: `item.create` with an `inbox` block is the seam (`internal/vault/vault.go`), and the command has to choose what `--body -` means for a piped body and which project an unqualified title lands in. The first criterion stays unticked until it exists.

`inbox.go` also holds the seam both this tree and `gintrack sprint` reach the core through — `openSpace` / `dispatch` / `vaultError` — which maps the vault error catalogue onto the documented exit codes.
