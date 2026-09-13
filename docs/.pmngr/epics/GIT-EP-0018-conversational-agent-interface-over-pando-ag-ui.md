---
id: GIT-EP-0018
type: epic
title: Conversational agent interface over Pando AG-UI
status: backlog
priority: high
milestone: GIT-M-0013
author: mcp
labels: [web, server, mcp, docs]
created: 2026-09-13T13:08:26Z
updated: 2026-09-13T13:08:26Z
---

## Description

A chat panel in the web app that talks to a Pando agent through the AG-UI protocol. The companion proxies `POST /api/v1/agent/run` to Pando's `POST /api/v1/agui/{agent}` (SSE), injecting the Pando bearer token server-side so it never reaches the browser, and authenticating the caller with the existing companion token. The frontend uses `@ag-ui/client` and a custom chat UI in the shadcn design system: streaming text, tool calls with collapsible results, shared state (todos, sub-agents, token usage) in a side panel, human-in-the-loop dialogs for `pando_permission_request` and questions, and frontend tools (`open_item`, `open_kb_page`, `focus_board_card`, `apply_filter`) that let the agent drive the UI. Pando reaches git-in-track's data through `[MCPServers.gintrack]` pointing at the companion's `/mcp` endpoint; a backlog-assistant persona and skill constrain the coder agent.

## Acceptance Criteria

- [ ] Companion config for Pando (`agent.pando: {url, token, agent}`), capability `features.agent`, health check `GET /api/v1/agent/info`.
- [ ] Go proxy for the SSE run endpoint, thread and run ids owned by the frontend, resume of interrupted runs with trailing `tool` messages.
- [ ] `web/src/features/agent/`: route, chat store, message list with Markdown, tool-call cards, state panel, permission and question dialogs, frontend tools registry.
- [ ] Pando side: `.pando.toml` template with `[AGUI]`, `[MCPServers.gintrack]`, persona `backlog-assistant.md`, skill with the search routing table; documented in a new `docs/20-agent-interface.md`.
- [ ] `gintrack serve --agent` starts with the feature; browser-only mode hides it.
- [ ] Dependency `@ag-ui/client` justified in the PR; no CopilotKit runtime, no Node sidecar.

## Notes

Pando reference: `internal/agui/{server,input,events,hitl,frontend_tool}.go`, `sdk/typescript/src/agui/client.ts`, `examples/copilotkit/app/page.tsx` for the interaction model.
