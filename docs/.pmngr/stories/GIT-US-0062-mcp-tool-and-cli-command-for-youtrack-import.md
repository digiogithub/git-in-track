---
id: GIT-US-0062
type: story
title: MCP tool and CLI command for YouTrack import
status: in_review
priority: medium
parent: GIT-EP-0012
milestone: GIT-M-0011
author: mcp
labels: [mcp, cli, docs]
estimate: 5
created: 2026-09-13T13:12:44Z
updated: 2026-09-13T15:41:40Z
started: 2026-09-13T15:41:31Z
---

## Description

As an agent or a scripter, I want `import_youtrack_issues` over MCP and `gintrack youtrack import` on the command line, so that an import can be driven without the web UI and can be reproduced in a shell script.

Add `internal/mcp/tools_youtrack.go` with a `registerYouTrackTools(s)` called from `registerTools` (`internal/mcp/tools.go:24`). The tool is a write tool, so it is absent from `tools/list` on a read-only server; its `In` struct carries `{project, query?, ids?, depth, includeLinks, includeComments, includeAttachments, dryRun}` with `jsonschema` tags, and `dryRun: true` dispatches `youtrack.import.preview` while `false` dispatches `youtrack.import.run`. The handler validates with `invalidField`, dispatches through `dispatch[T]`, projects the result through `internal/mcp/wire.go` so every returned item carries `id` and `rev`, and calls `s.announce` on a write. The surface-pinning tests in `internal/mcp/tools_test.go:16-26` and `cmd/gintrack/mcp_test.go:28,37-40` must be updated or they fail.

Add `cmd/gintrack/youtrack.go` with a `youtrack` parent command and an `import <query|ids...>` subcommand, registered in `root.go:85-100` and using the `exit.go` arg validators so a bad invocation exits 2. Flags: `--depth`, `--comments`, `--attachments`, `--links`, `--dry-run`, `--json`. The command resolves config with `flags.resolve()`, builds a printer with `flags.printer(cmd, asJSON)` and opens the vault with `openVault(...)`; the table output lists issue, action and target id, and human lines go to stderr in JSON mode so `… --json | jq` stays safe. No business logic in the command file.

## Acceptance Criteria

- [ ] `import_youtrack_issues` is registered as a write tool, hidden on a read-only server, with `dryRun` selecting preview vs run.
- [ ] The MCP surface-pinning tests and `cmd/gintrack/mcp_test.go` are updated and pass.
- [ ] `gintrack youtrack import <query|ids...>` runs with `--depth`, `--comments`, `--attachments`, `--links`, `--dry-run` and `--json`.
- [ ] Table output lists issue, action and resulting item id; `--json` emits the raw result with notes on stderr.
- [ ] Errors carry the machine-readable `{"error":{…}}` payload over MCP and a documented exit code on the CLI.
- [ ] `docs/08-mcp-server.md` §4 and `docs/07-cli-and-api.md` §4 document the tool and the command; `CHANGELOG.md` gains an entry.
- [ ] `go test -race ./internal/mcp/... ./cmd/gintrack/...` passes, including a behaviour test through the in-memory MCP client harness.

## Notes

Depends on the vault operations and the import job kind of this epic. The MCP checklist is the scratchpad gintrack report §5.5; the CLI command recipe is §6. `internal/mcp` must stay free of business logic — it only validates, dispatches and projects.

Known doc drift to fix while in there: `docs/07-cli-and-api.md:896-916` lists 12 tools and says "six write tools"; there are 13 and 7.

Do NOT add a transport or schema file — the SDK infers both JSON schemas by reflection over `In`/`Out`. Do NOT let the CLI block forever on a long import; poll the job and honour interrupt.
