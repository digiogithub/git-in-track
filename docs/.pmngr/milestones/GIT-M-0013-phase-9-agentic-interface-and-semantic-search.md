---
id: GIT-M-0013
type: milestone
title: Phase 9 — Agentic interface and semantic search
status: backlog
author: mcp
labels: [web, server, mcp, docs]
created: 2026-09-13T13:06:59Z
updated: 2026-09-13T13:06:59Z
due: 2027-01-31
---

## Description

A conversational agent panel inside the web app, speaking the AG-UI protocol to a Pando instance through a companion-side proxy, with git-in-track's own MCP tools available to the agent, human-in-the-loop permission prompts and frontend tools that drive the UI. Alongside it, semantic search: git-in-track exports backlog items and KB pages to a corpus Pando indexes, and queries Pando's hybrid search over MCP as an optional native search accelerator behind the existing `core/search` contract.

Built with `@ag-ui/client` and the existing shadcn design system; no Node sidecar, no CopilotKit runtime. Companion mode only.

## Acceptance Criteria

- [ ] An agent chat route exists, streams AG-UI events from Pando via the companion, renders text, tool calls and shared state, and handles interrupts (permissions, questions, frontend tools).
- [ ] Pando can call git-in-track's MCP tools from the chat; the agent is constrained by a backlog-assistant persona and skill.
- [ ] Items and KB pages are exported to a Pando KB corpus kept in sync on file changes; semantic search results appear in the app's search and in the chat.
- [ ] Everything degrades cleanly in browser-only mode and when Pando is not reachable.

## Notes

Due date is a planning estimate. Analysis source: `/www/MCP/Pando/pando` (`internal/agui`, `sdk/typescript/src/agui`, `internal/rag/kb`, `internal/app/remembrances.go`).
