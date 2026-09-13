---
id: GIT-US-0069
type: story
title: Pando side configuration, persona and agent interface documentation
status: backlog
priority: medium
parent: GIT-EP-0018
milestone: GIT-M-0013
author: mcp
labels: [cli, docs]
estimate: 5
created: 2026-09-13T13:13:20Z
updated: 2026-09-13T13:13:20Z
---

## Description

As someone setting this up for the first time, I want `gintrack agent init` to write a working `.pando.toml` and persona, so that connecting a Pando instance is a documented two-command procedure rather than reverse engineering two products.

Add `cmd/gintrack/agent.go` with an `agent init` subcommand that writes, next to the repository, a `.pando.toml` containing `[AGUI]` (`Enabled`, `Agents = ['coder']`, `AllowedOrigins` with the companion origin, `RequireToken = true`, `Persona`), `[MCPServers.gintrack]` as `Type = 'streamable-http'` pointing at the companion's `/mcp` with the companion bearer token in `[MCPServers.gintrack.Headers]`, and `[Remembrances]` with `KBPath`, `KBAutoImport` and `KBWatch` for the corpus the semantic-search epic exports. It also writes `agents/personas/backlog-assistant.md` and a skill file carrying the tool routing table (structured questions to the gintrack MCP tools, semantic to `kb_search_documents`, code to `code_hybrid_search`). Existing files are never overwritten without `--force`, and the command refuses to print or store the Pando token in the repository.

Write `docs/20-agent-interface.md`: the architecture (browser → companion proxy → Pando AG-UI; Pando → companion `/mcp`), the setup procedure, the configuration reference, the frontend tool list, the security model (two tokens, two directions, neither in the browser) and the troubleshooting table. Add `docs/adr/ADR-035-agent-interface-over-ag-ui.md` recording the choice of AG-UI plus a custom chat UI over CopilotKit with a Node sidecar, with the `@ag-ui/client` dependency and the operational coupling to a per-project Pando instance as explicit negative consequences.

## Acceptance Criteria

- [ ] `gintrack agent init` writes `.pando.toml`, the persona and the skill file, and prints the next command to run.
- [ ] Re-running without `--force` leaves existing files untouched and exits non-zero with a clear message.
- [ ] The generated `[MCPServers.gintrack]` block makes the gintrack tools callable from an AG-UI run with no Pando code change.
- [ ] No token value is written into any file inside the repository; secrets come from the companion config or the environment.
- [ ] `docs/20-agent-interface.md` exists and covers architecture, setup, configuration, tools, security and troubleshooting.
- [ ] `docs/adr/ADR-035-*.md` is accepted, states the alternatives considered and lists negative consequences.
- [ ] `docs/07-cli-and-api.md` gains the `gintrack agent init` command and `CHANGELOG.md` records the feature.
- [ ] `go test -race ./cmd/gintrack/...` covers generation, `--force` and the refusal path against a temp directory.

## Notes

`gintrack serve --mcp-http [--mcp-allow-write]` already mounts `POST /mcp` (`internal/server/mcp.go:25`, flags `cmd/gintrack/serve.go:87-92`), and Pando passes its MCP gateway into AG-UI runs automatically (`internal/agui/agentpool.go:82-90`) — so no Pando change is needed.

Caveat to document: the AG-UI agent is Pando's full **coder** agent with bash, edit and write tools. There is no per-AG-UI-agent tool allow-list in `AGUIConfig`; constraining it to a backlog assistant is a persona plus skill convention, not an enforcement boundary. Say so plainly in the doc and in the ADR's negative consequences.

`pando-schema.json` is stale (it omits `AGUI`, `Remembrances`, `MCPServer`); the authority for the TOML shape is Pando's `internal/config/config.go:1137-1186`. `pando agui-serve` prints a generated token to stdout (`cmd/agui_serve.go:174-175`) — the doc should tell the user to put it in the companion config, not in the repo. Pando's MCP HTTP transport has no authentication, so bind it to loopback.

ADR numbering: check `docs/adr/` for the highest number before writing; ADRs are immutable once accepted.
