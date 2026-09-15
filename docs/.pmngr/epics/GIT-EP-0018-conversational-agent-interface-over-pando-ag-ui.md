---
id: GIT-EP-0018
type: epic
title: Conversational agent interface over Pando AG-UI
status: in_progress
priority: high
milestone: GIT-M-0013
author: mcp
labels: [web, server, mcp, docs]
created: 2026-09-13T13:08:26Z
updated: 2026-09-15T17:09:51Z
started: 2026-09-15T17:09:51Z
---

## Description

A chat panel in the web app that talks to a Pando agent through the AG-UI protocol. The companion proxies `POST /api/v1/agent/run` to Pando's `POST /api/v1/agui/{agent}` (SSE), injecting the Pando bearer token server-side so it never reaches the browser, and authenticating the caller with the existing companion token. The frontend uses `@pando-ai/sdk/agui` and a custom chat UI in the shadcn design system: streaming text, tool calls with collapsible results, shared state (todos, sub-agents, token usage) in a side panel, human-in-the-loop dialogs for `pando_permission_request` and questions, and frontend tools (`open_item`, `open_kb_page`, `focus_board_card`, `apply_filter`) that let the agent drive the UI. Pando reaches git-in-track's data through `[MCPServers.gintrack]` pointing at the companion's `/mcp` endpoint; a backlog-assistant persona and skill constrain the coder agent.

**Deployment shape (settled):** one `pando agui-serve` process per repository, and the companion owns a routing table `projectId -> {url, token}`. Never `pando serve --agui-port` co-mounted — that exposes `/api/v1/files`, `/terminal/exec` and `/config/*` on the same listener — and never one process serving several working directories (Pando's config and DB are process-global, and two RW SQLite writers in one cwd is a corruption path).

**Identity and tokens (settled):** git-in-track owns identity and multi-tenancy. The Pando token is a service credential held by the companion only; it is never `?token=` and never reaches the browser. Pando's `AllowedOrigins` stays empty because the proxy strips the browser `Origin` header.

## Acceptance Criteria

- [ ] Companion config for Pando (`agent.pando: {url, token, agent}` per project), capability `features.agent`, health check `GET /api/v1/agent/info`.
- [ ] A `projectId -> {url, token}` routing table selects the right `agui-serve` process per request; one process per repository, never a shared multi-cwd process.
- [ ] Go proxy for the SSE run endpoint, thread and run ids owned by the frontend, resume of interrupted runs with trailing `tool` messages.
- [ ] The proxy imposes its own concurrency cap — a per-user and a global in-flight run limit — and refuses over the cap with a typed problem plus `Retry-After`. Pando enforces no limit of its own today.
- [ ] `web/src/features/agent/`: route, chat store, message list with Markdown, tool-call cards, state panel, permission and question dialogs, frontend tools registry.
- [ ] Thread ids are persisted by git-in-track itself (companion-side, per conversation); the Pando session id may be reused as the `threadId`. Reload and reconnect of an existing thread wait on PANDO-EP-0003.
- [ ] Pando side: `.pando.toml` template with `[AGUI]`, `[MCPServers.gintrack]`, persona `backlog-assistant.md`, skill with the search routing table; documented in a new `docs/20-agent-interface.md`.
- [ ] `gintrack serve --agent` starts with the feature; browser-only mode hides it.
- [ ] Dependency `@pando-ai/sdk/agui` justified in the PR; no CopilotKit runtime, no Node sidecar, no `@ag-ui/client`, no vendored copy of the SDK transport.

## Notes

**Client library.** The web app depends on `@pando-ai/sdk/agui`, not `@ag-ui/client` (which loses `outcome`, `REASONING_*` and `ACTIVITY_*` and pulls rxjs/zod/proto into the bundle) and not a vendored copy. This blocks on PANDO-EP-0001, which owns the browser-safe build of that SDK; the reducer, JSON-Patch and HITL helpers come from the same epic.

**Interim tool restriction.** Until PANDO-EP-0002 ships the adapter-wide `[AGUI] Tools` allow-list plus `Mesnada = false`, the only restriction available is human-in-the-loop: `HumanInTheLoop = true` with `AutoApprove = false`, so every dangerous call needs a browser approval. That is an interim posture, not the end state — the agent is still Pando's full coder agent with bash, edit and write. Say so in the ADR's negative consequences. Running `agui-serve` as a low-privilege user with a read-only bind mount is a worthwhile second layer.

**Thread durability.** A dropped stream cancels the turn today, which will happen daily in a browser panel. Thread list/messages/delete, `MESSAGES_SNAPSHOT`, park-on-disconnect, reattach and cancel are funded in PANDO-EP-0003.

**Pando dependencies (plain ids; cross-project links are not supported):**
- PANDO-EP-0001 — browser-safe `@pando-ai/sdk/agui` build, reducer/JSON-Patch/HITL helpers. Gates GIT-US-0053.
- PANDO-EP-0002 — `[AGUI] Tools` allow-list + `Mesnada = false`. Gates the generated `.pando.toml` in GIT-US-0069; until then HITL is the boundary (GIT-US-0061).
- PANDO-EP-0003 — thread lifecycle and run durability. Gates reload/reconnect of a thread.
- PANDO-EP-0004 — AG-UI operability (health endpoint). Until it lands, health is `GET /info`.

Pando reference: `internal/agui/{server,input,events,hitl,frontend_tool}.go`, `sdk/typescript/src/agui/client.ts`. CopilotKit is out: it mandates a Node process speaking GraphQL that Pando refuses to implement on purpose (`internal/agui/doc.go`).
