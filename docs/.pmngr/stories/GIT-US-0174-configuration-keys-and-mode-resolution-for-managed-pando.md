---
id: GIT-US-0174
type: story
title: Configuration keys and mode resolution for managed Pando
status: done
priority: high
parent: GIT-EP-0031
assignees: [claude]
author: mcp
labels: [server, cli, docs, agent-ok]
created: 2026-09-29T18:53:52Z
updated: 2026-09-29T20:58:55Z
started: 2026-09-29T18:55:16Z
closed: 2026-09-29T20:58:55Z
---

## Description

Adds the configuration surface of ADR-039 and the six-rule mode resolution table (first match wins: `off`; `external`; `managed`; `auto` + `mcpUrl` → external; `auto` + binary on PATH → managed; `auto` without binary → off).

## Acceptance Criteria

- [x] `search.pando.mode` (default `auto`), `search.pando.managed.*` and machine-local `repos[].semanticSearch` are parsed and validated.
- [x] Resolution follows the ADR table; `mode: managed` mixed with external keys is refused naming the keys; a missing binary resolves to off/`unavailable`, never an error.
- [x] `gintrack doctor` reports the resolved mode and the rule that applied.
- [x] Table-driven tests cover every rule; docs/07 §3.3 updated.
