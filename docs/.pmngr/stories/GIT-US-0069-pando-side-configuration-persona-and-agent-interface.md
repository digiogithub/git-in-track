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
updated: 2026-09-13T21:16:16Z
---

## Description

As someone setting this up for the first time, I want `gintrack agent init` to write a working `.pando.toml` and persona, so that connecting a Pando instance is a documented two-command procedure rather than reverse engineering two products.

Add `cmd/gintrack/agent.go` with an `agent init` subcommand that writes, next to the repository, **one `.pando.toml` per repository** — the deployment is one `pando agui-serve` process per repository, so there is exactly one config file per repository and no multi-cwd variant.

The generated file contains:

- `[AGUI]` with `Enabled`, `Agents = ['coder']`, `RequireToken = true`, `Persona`, **`AllowedOrigins` left empty** (the companion proxy strips the browser `Origin` header and Pando skips its CORS check when the header is absent — do not list the companion origin), `HumanInTheLoop = true` and `AutoApprove = false`.
- `[AGUI] Tools` — an explicit allow-list of the tools the backlog assistant may call — together with `Mesnada = false`, emitted **once PANDO-EP-0002 ships those keys**. Until then the generator omits them and writes a comment saying that HITL plus `AutoApprove = false` is the interim boundary and that the agent otherwise has the full coder toolset.
- `[MCPServers.gintrack]` as `Type = 'streamable-http'` pointing at the companion's `/mcp` with the companion bearer token in `[MCPServers.gintrack.Headers]`.
- `[Remembrances]` with `KBPath`, `KBAutoImport = true` and **`KBWatch = false`** for the corpus the semantic-search epic exports. The watcher is what destroys metadata on every edit, so it stays off; re-sync is driven by the companion (today: wait for the next auto-import pass; later: Pando's REST reindex route, PANDO-EP-0005). The generator writes a comment saying why.
- `[MCPServer]` with `HttpEnabled` **off** unless something actually needs it, plus a comment that Pando's own MCP HTTP transport on `:9777` is unauthenticated with CORS `*` and global auto-approve, so enabling it exposes it to any web page the user visits (PANDO-EP-0006).

It also writes `agents/personas/backlog-assistant.md` and a skill file carrying the tool routing table (structured questions to the gintrack MCP tools, semantic to `kb_search_documents`, code to `code_hybrid_search`). Existing files are never overwritten without `--force`, and the command refuses to print or store the Pando token in the repository.

Write `docs/20-agent-interface.md`: the architecture (browser → companion proxy → per-repository `pando agui-serve`; Pando → companion `/mcp`), the setup procedure, the configuration reference, the frontend tool list, the security model (two tokens, two directions, neither in the browser) and the troubleshooting table. Add `docs/adr/ADR-035-agent-interface-over-ag-ui.md` recording the choice of AG-UI plus a custom chat UI over CopilotKit with a Node sidecar, with the `@pando-ai/sdk/agui` dependency and the operational coupling to a per-repository Pando instance as explicit negative consequences.

## Acceptance Criteria

- [ ] `gintrack agent init` writes one `.pando.toml` for the repository, the persona and the skill file, and prints the next command to run.
- [ ] The generated `[Remembrances]` block sets `KBAutoImport = true` and `KBWatch = false`, with a comment explaining that the watcher erases front-matter metadata.
- [ ] The generated `[AGUI]` block leaves `AllowedOrigins` empty and sets `HumanInTheLoop = true`, `AutoApprove = false`.
- [ ] Once PANDO-EP-0002 exists, the generated `[AGUI]` block carries a `Tools` allow-list and `Mesnada = false`; before that it carries the equivalent comment and the interim-boundary warning.
- [ ] `[MCPServer] HttpEnabled` is off in the generated file, with a comment that `:9777` is unauthenticated, CORS `*` and globally auto-approving.
- [ ] Re-running without `--force` leaves existing files untouched and exits non-zero with a clear message.
- [ ] The generated `[MCPServers.gintrack]` block makes the gintrack tools callable from an AG-UI run with no Pando code change.
- [ ] No token value is written into any file inside the repository; secrets come from the companion config or the environment.
- [ ] `docs/20-agent-interface.md` exists and covers architecture, setup, configuration, tools, security and troubleshooting.
- [ ] `docs/adr/ADR-035-*.md` is accepted, states the alternatives considered and lists negative consequences — including that loopback binding is not a boundary against the user's own browser while Pando's CORS policy is `*`.
- [ ] `docs/07-cli-and-api.md` gains the `gintrack agent init` command and `CHANGELOG.md` records the feature.
- [ ] `go test -race ./cmd/gintrack/...` covers generation, `--force` and the refusal path against a temp directory.

## Notes

`gintrack serve --mcp-http [--mcp-allow-write]` already mounts `POST /mcp` (`internal/server/mcp.go:25`, flags `cmd/gintrack/serve.go:87-92`), and Pando passes its MCP gateway into AG-UI runs automatically (`internal/agui/agentpool.go:82-90`) — so no Pando change is needed for the tool wiring.

**Caveat to document.** The AG-UI agent is Pando's full **coder** agent with bash, edit and write tools. There is no per-AG-UI-agent tool allow-list in `AGUIConfig` today; constraining it to a backlog assistant is a persona plus skill convention, not an enforcement boundary. PANDO-EP-0002 makes it one. Say so plainly in the doc and in the ADR's negative consequences.

**Security note, stated correctly.** "Bind the MCP HTTP transport to loopback" is true but insufficient: Pando's CORS policy on that listener is `*` with `SetGlobalAutoApprove(true)`, so loopback binding does **not** protect the user from a web page they visit. The generated doc and ADR-035 must say that plainly, and the ADR's negative consequences must list it. PANDO-EP-0006 authenticates that transport.

`pando-schema.json` is stale (it omits `AGUI`, `Remembrances`, `MCPServer`); the authority for the TOML shape is Pando's `internal/config/config.go`. `pando agui-serve` prints a generated token to stdout (`cmd/agui_serve.go:174-175`) — the doc should tell the user to put it in the companion config, not in the repo, and the companion keys it by project id.

**Pando dependencies (plain ids):** PANDO-EP-0002 (tool allow-list + `Mesnada`) — unblocks the real restriction in the template. PANDO-EP-0006 (authenticate the MCP HTTP transport) — makes `HttpEnabled` safe to recommend. PANDO-EP-0005 (REST reindex) — replaces "wait for the next auto-import" in the corpus setup section.

ADR numbering: check `docs/adr/` for the highest number before writing; ADRs are immutable once accepted.
