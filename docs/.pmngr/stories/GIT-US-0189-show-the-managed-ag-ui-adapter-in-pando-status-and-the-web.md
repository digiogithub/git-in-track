---
id: GIT-US-0189
type: story
title: Show the managed AG-UI adapter in pando status and the web
status: backlog
priority: low
parent: GIT-EP-0031
author: mcp
labels: [cli, web, agent-ok]
created: 2026-09-30T12:58:02Z
updated: 2026-09-30T12:58:02Z
---

## Description

GIT-US-0185 (#115) runs one managed `pando agui-serve` per opted-in repo and reports it as `indexed[].managed.agui` in the search settings API. `gintrack pando status` does not list the adapter, and the web shows nothing for it. Each opted-in repo now runs two Pando processes (about 170–420 MB each), and the adapter keeps its own KB index, so documentation embeddings are computed twice.

## Acceptance Criteria

- [ ] `gintrack pando status [--json]` lists the adapter per repo (state, pid, port, version, error; never the token).
- [ ] The search settings card shows the adapter state with a restart control; browser-only mode hides it.
- [ ] Investigate sharing the KB index between the search instance and the adapter (or a single process); record the finding in docs/21 and, if feasible without an upstream change, implement it.
- [ ] Tests for CLI output and the card state.
