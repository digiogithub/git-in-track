---
id: GIT-US-0185
type: story
title: Supervise pando agui-serve for the agent panel
status: done
priority: medium
parent: GIT-EP-0031
assignees: [claude]
author: mcp
labels: [server, web, agent-ok]
created: 2026-09-30T07:00:52Z
updated: 2026-09-30T12:58:09Z
started: 2026-09-30T12:35:38Z
closed: 2026-09-30T12:58:09Z
---

## Description

ADR-039 open question 4, decided 2026-09-30: the managed-Pando supervisor also runs `pando agui-serve` for the agent panel (ADR-035), instead of the user starting it by hand.

## Acceptance Criteria

- [x] In managed mode, `gintrack serve` starts and supervises `agui-serve` (lifecycle, health, backoff, state file) with the same rules as the MCP instance; which repo/instance it serves is documented.
- [x] The agent panel discovers the managed AG-UI endpoint without manual configuration; external setups keep working.
- [x] Tests use a fake binary; docs/21, docs/05 and ADR-035 cross-references are updated.

## Notes

Done in PR #115; a security review found no blocking issue. There is one adapter per opted-in repo in `<cache>/pando/<key>-agui/`, reusing the supervisor (`Options.Kind = KindAGUI`), and the config templates moved to `internal/agentcfg`.

The proxy resolves the managed adapter, then `agent.pando.repos`, then `agent.pando.url`. A not-ready adapter returns 503 with `Retry-After` and never falls back to another repo's upstream. `agent.pando.managed: false` opts out.

The CLI status, the web card and the double KB index move to GIT-US-0189.
