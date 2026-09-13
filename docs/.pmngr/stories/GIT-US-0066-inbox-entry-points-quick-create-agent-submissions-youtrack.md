---
id: GIT-US-0066
type: story
title: "Inbox entry points: quick create, agent submissions, YouTrack landing and CLI"
status: done
priority: medium
parent: GIT-EP-0016
milestone: GIT-M-0012
author: mcp
labels: [web, cli, mcp]
estimate: 5
created: 2026-09-13T13:13:05Z
updated: 2026-09-13T17:36:14Z
started: 2026-09-13T17:27:14Z
closed: 2026-09-13T17:36:14Z
---

## Description

As a person capturing an idea, a bug report or a request, I want a one-field way to put it in the inbox from the web app, the terminal, an agent or a YouTrack import, so that nothing is lost to "I will write it up properly later" and nobody has to answer type, parent and status questions at capture time.

Four surfaces, one underlying call. In the web app, an "Add to inbox" action next to the existing new-item entry points (`web/src/features/backlog/NewItemLink.tsx`, the AppShell header) opens a small dialog with title, an optional body and nothing else, and posts `createInboxItem`. For agents, the MCP tool `create_inbox_item` (built in the operations story) is the documented way to hand work to a human instead of writing straight into the backlog — its description says so, and `docs/08-mcp-server.md` §4 gets a paragraph on when an agent should choose it over `create_story`. For the terminal, a new `cmd/gintrack/inbox.go` adds `gintrack inbox list|add|accept|reject|snooze`, built exactly like the other commands: `newInboxCommand(flags *globalFlags)`, the arg validators from `cmd/gintrack/exit.go:86-106`, `flags.resolve()` → `flags.printer(cmd, asJSON)` → `openVault(...)`, registered in `cmd/gintrack/root.go:85-100`, with `--json` per command and human notes on stderr (`cmd/gintrack/output/output.go:126`).

The fourth entry point is the YouTrack importer: a per-project `integrations.youtrack.land_in_inbox` option that makes imported issues arrive in triage instead of the backlog, so a large import can be reviewed before it becomes commitments. The importer work itself is GIT-EP-0012; this story only defines the config key, documents it and makes the importer's write path honour an "inbox" target, so the two epics can land independently.

## Acceptance Criteria

- [x] An "Add to inbox" quick-create dialog exists in the web app (title + optional body), creates a pending inbox item and shows a link to the Inbox page.
- [x] `gintrack inbox list [--status pending|snoozed|rejected|accepted|duplicate|all] [--json]` prints the queue as a table and as JSON, with human notes on stderr.
- [x] `gintrack inbox add <title> [--body|-]`, `inbox accept <id> --status <s> [--type] [--parent]`, `inbox reject <id>` and `inbox snooze <id> --until <date>` all work and return the documented exit codes.
- [x] `create_inbox_item`'s MCP description tells an agent when to use it instead of `create_story`, and `docs/08-mcp-server.md` §4 documents the three inbox tools.
- [x] `integrations.youtrack.land_in_inbox` is declared on `core.ProjectConfig` and documented; when set, imported items are created with the project's triage status and `inbox.source: youtrack`.
- [x] `docs/07-cli-and-api.md` §4 documents the `inbox` command tree and the REST endpoints.
- [x] `go test -race ./cmd/gintrack/...` covers the command tree with the in-process harness, and Vitest covers the quick-create dialog.

## Notes

Existing code: `cmd/gintrack/root.go:50-100` (command registration), `cmd/gintrack/item.go:21-41` (the closest command tree to copy), `cmd/gintrack/workspace.go:78-173` (`openVault`/`project`/`store`), `cmd/gintrack/harness_test.go:28` (in-process test harness), `cmd/gintrack/output/output.go:103-162` (`Printer`), `internal/core/project.go:23-40` (`ProjectConfig` — note it has no `Extra` map, so a new key must be declared to be visible).

Link this story to GIT-EP-0012 (YouTrack import) in review: the importer owns the fetching, this story owns only the config key and the triage landing target.

Do NOT add business logic to the cobra command files (`cmd/gintrack/main.go:5-7`); they resolve config, open the vault and print. Do NOT rewrite `project.yaml` wholesale from Go — the only writer today is the surgical `Allocator.writeCounter` (`internal/core/allocator.go:414`) and a full re-serialize would drop hand-added blocks.

### The fifth criterion, and the one correction it needed

Its first clause is satisfied, with one correction: the key could not be declared on `core.ProjectConfig`, because that struct carries no `integrations` block at all. The block is modelled by `config.YouTrackLink` in `internal/config/projectlink.go`, deliberately — the core struct models no unknown keys and `project.yaml` carries hand-written sections this build does not know about, so the block is read and edited node by node. `LandInInbox bool` / `yaml:"land_in_inbox,omitempty"` landed there, defaults to false, decodes, survives a connection save, and is documented in `docs/03-data-model.md` §6 as R-INT-7. `core.InboxLandingStatus` is the shared landing decision and refuses the "land in the inbox, but this project has no inbox" combination rather than falling back silently.

Its second clause — "when set, imported items are created with the project's triage status and `inbox.source: youtrack`" — was the importer half the description assigns to GIT-EP-0012, and it landed as **GIT-T-0226**: `vault.YouTrackLink.LandInInbox` carries the setting across the WASM boundary, `youtrackLinkOf` populates it, and the importer forces the triage status plus `inbox: {status: pending, source: youtrack}` onto the draft of an issue it creates. An issue it updates keeps its status, and a project that asks for the inbox without declaring a triage status has the whole import refused. The criterion is ticked on that basis.
