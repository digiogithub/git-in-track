---
id: GIT-US-0102
type: story
title: Filter workspace search by project
status: in_review
priority: medium
parent: GIT-EP-0021
milestone: GIT-M-0014
author: mcp
labels: [core, server, web]
estimate: 3
created: 2026-09-17T09:31:58Z
updated: 2026-09-17T09:47:05Z
started: 2026-09-17T09:33:20Z
---

## Description

As a user of a workspace with several projects, I want to restrict the workspace search to one or more projects. All projects are selected by default.

## Acceptance Criteria

- [ ] `SearchQuery` gains `projectKeys?: string[]` (the single `projectKey` keeps working); the core, the companion route and the semantic (Pando) half all honour it.
- [ ] `WorkspaceSearch` shows a multi-select of projects, all checked by default, with "all" / "none" shortcuts; the selection survives a query change.
- [ ] An empty selection shows a hint instead of querying.
- [ ] Tests: core filter, server query parsing, component.
- [ ] docs/07 updated.
