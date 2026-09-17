---
id: GIT-M-0013
type: milestone
title: Phase 9 — Agentic interface and semantic search
status: done
author: mcp
labels: [web, server, mcp, docs]
created: 2026-09-13T13:06:59Z
updated: 2026-09-16T16:00:43Z
started: 2026-09-15T17:09:10Z
closed: 2026-09-16T16:00:43Z
due: 2027-01-31
---

## Description

A conversational agent panel inside the web app, speaking the AG-UI protocol to a Pando instance through a companion-side proxy, with git-in-track's own MCP tools available to the agent, human-in-the-loop permission prompts and frontend tools that drive the UI. Alongside it, semantic search: git-in-track exports backlog items and KB pages to a corpus Pando indexes, and queries Pando's hybrid search over MCP as an optional native search accelerator behind the existing `core/search` contract.

Built with `@pando-ai/sdk/agui` and the existing shadcn design system; no Node sidecar, no CopilotKit runtime. Companion mode only. The deployment is one `pando agui-serve` process per repository, routed by the companion.

## Acceptance Criteria

- [ ] An agent chat route exists, streams AG-UI events from Pando via the companion, renders text, tool calls and shared state, and handles interrupts (permissions, questions, frontend tools).
- [ ] Pando can call git-in-track's MCP tools from the chat; the agent is constrained by a backlog-assistant persona and skill, with HITL as the enforced boundary until Pando's tool allow-list exists.
- [ ] Items and KB pages are exported to a Pando KB corpus kept current by the companion; semantic search results appear in the app's search and in the chat.
- [ ] Everything degrades cleanly in browser-only mode and when Pando is not reachable.

## Notes

Due date is a planning estimate. Analysis source: `/www/MCP/Pando/pando` (`internal/agui`, `sdk/typescript/src/agui`, `internal/rag/kb`, `internal/app/remembrances.go`).

**Pando dependencies.** Cross-project links are not supported, so these are plain ids. Four of the seven PANDO epics gate work here; the rest are quality-of-life.

- **PANDO-EP-0001** (TypeScript SDK, browser-first AG-UI client) — hard blocker on **GIT-US-0053**, which depends on `@pando-ai/sdk/agui` shipping a browser-safe build, and supplies the reducer, JSON-Patch and HITL helpers that **GIT-US-0061** consumes. Milestone PANDO-M-0001.
- **PANDO-EP-0002** (`[AGUI] Tools` allow-list + `Mesnada = false`) — gates the real tool restriction in **GIT-US-0069**'s generated `.pando.toml`. Until it lands, **GIT-US-0061**'s HITL dialogs plus `AutoApprove = false` *are* the security boundary, and **GIT-EP-0018** ships with that caveat in its ADR. Milestone PANDO-M-0001.
- **PANDO-EP-0003** (thread lifecycle and run durability: thread list/messages/delete, `MESSAGES_SNAPSHOT`, park-on-disconnect, reattach, cancel) — gates transcript restore on reload and removes **GIT-US-0053**'s one-thread-per-tab plus `BroadcastChannel` workaround. git-in-track persists its own thread id per conversation regardless. Milestone PANDO-M-0001.
- **PANDO-EP-0004** (AG-UI operability for embedded deployments) — replaces **GIT-US-0049**'s `GET /info` health probe with a real health endpoint, and is where a server-side concurrency limit would live; until then the proxy imposes its own per-user and global in-flight cap. Milestone PANDO-M-0001.
- **PANDO-EP-0005** (KB metadata fidelity and REST search surface) — would restore the front-matter round trip for **GIT-US-0073** and give **GIT-US-0091** a real awaitable KB reindex with counts, and **GIT-US-0082** a server-side path-prefix filter plus a plain-HTTP alternative to MCP. None of these block: the design deliberately treats Pando hits as candidates and re-reads authoritative fields locally. Milestone PANDO-M-0002.
- **PANDO-EP-0006** (authenticate the MCP HTTP transport) — `:9777` is unauthenticated, CORS `*`, globally auto-approving. **GIT-US-0069** generates `HttpEnabled` off and **GIT-US-0077** refuses non-loopback URLs because of it; neither is blocked. Milestone PANDO-M-0002.
- **PANDO-EP-0007** (search scale and correctness) — removes **GIT-US-0082**'s chunk-count ceiling (KB vector search is a full scan per query: ~100-200 ms at ~10 000 chunks, unacceptable at ~35 000) and its fallback to the core index. Not a blocker. Milestone PANDO-M-0002.

**Everything else in this milestone is buildable today.** GIT-US-0049 (proxy), GIT-US-0057, GIT-US-0064, GIT-US-0069, GIT-US-0073 (exporter), GIT-US-0077 (MCP client — transport verified against go-sdk v1.4.1), GIT-US-0082 (with the over-fetch workaround), GIT-US-0086, GIT-US-0088 and GIT-US-0091's backend half need no Pando change to start. GIT-US-0053 is the only story that cannot begin before an upstream delivery.
