---
id: GIT-US-0172
type: story
title: Correct the Pando CORS default in docs/07 and internal/pando
status: done
priority: low
assignees: [claude]
author: mcp
labels: [docs, server, agent-ok, good-first-issue]
created: 2026-09-29T18:48:26Z
updated: 2026-09-29T22:30:37Z
started: 2026-09-29T22:21:52Z
closed: 2026-09-29T22:30:37Z
---

## Description

docs/07 §3.3 and the package comment in `internal/pando/doc.go` say Pando's HTTP MCP endpoint allows CORS `*`. Current Pando (source checked at commit b1ed27a4b while drafting ADR-039) ships an empty CORS allow-list by default. The docs should describe the current default and what a browser-side caller needs to configure, without claiming a wildcard.

## Acceptance Criteria

- [x] docs/07 §3.3 and `internal/pando/doc.go` describe the empty default allow-list and the setting that widens it.
- [x] The minimum Pando version with the new default is stated, or the text says it is unknown.
- [x] `make lint` passes.

## Notes

Found while researching ADR-039 (gintrack-managed Pando). Fixed in PR #105: the setting is `MCPServer.HttpAllowedOrigins` (empty by default since Pando commit 13f01347b, first tag v0.715.6). ADR-039 still quotes the old wildcard claim as context; ADR text was left unchanged.
