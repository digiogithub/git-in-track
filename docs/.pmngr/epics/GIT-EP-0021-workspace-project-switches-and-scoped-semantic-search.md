---
id: GIT-EP-0021
type: epic
title: Workspace project switches and scoped semantic search
status: backlog
priority: high
milestone: GIT-M-0014
author: mcp
labels: [web, server, core]
created: 2026-09-17T09:31:41Z
updated: 2026-09-17T09:31:41Z
---

## Description

Four capabilities for the web workspace:

1. A button on each project of the workspace list that turns the inbox on, by adding a `triage`-category status to that project's `project.yaml` (projects created before ADR-033 lack it).
2. A button on each repository/project whose Pando semantic index is `off` or `unavailable` that registers the repository with Pando and reindexes it (code + KB). When Pando is not configured at all, the button links to Settings.
3. A project filter on the workspace search: multi-select of projects, all selected by default.
4. A search overlay opened with Ctrl+Shift+F from any project view (backlog, inbox, KB), scoped to the current project, with All / Items / KB tabs, semantic + full-text as in the workspace search.

## Acceptance Criteria

- [ ] Every story of this epic is done and merged to main.
- [ ] Go tests, wasm build, vitest, typecheck and eslint pass on main.

## Notes

Decisions validated with the user on 2026-09-17: per-repo register+reindex (not the global reindex); filter unit is the project key (`SearchQuery.projectKeys[]`); overlay scope is the current project only.
