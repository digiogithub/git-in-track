---
id: GIT-EP-0031
type: epic
title: gintrack launches and manages Pando per opted-in repository
status: in_progress
priority: high
author: mcp
labels: [server, cli, web, docs]
created: 2026-09-29T18:53:18Z
updated: 2026-09-30T07:01:08Z
started: 2026-09-29T21:24:26Z
---

## Description

Implements ADR-039 (`docs/adr/ADR-039-gintrack-launches-and-manages-pando.md`). `gintrack serve` supervises one managed Pando instance per repository that opts in to semantic search (option C), so semantic search and impact tiers 2 and 3 work without a hand-run Pando.

Maintainer decisions (2026-09-29):
- One managed Pando per opted-in repo; no plan to consolidate into one instance.
- The opt-in is machine-local: `repos[].semanticSearch` in the user's gintrack config. No data-model change.
- Managed mode is the default when a `pando` binary is on PATH (`search.pando.mode: auto`); explicit `external`/`off` wins, and an existing `mcpUrl` keeps external mode.
- `gintrack mcp` never starts Pando nor proxies its tools; agents connect to Pando's own MCP directly, discovering the endpoint with `gintrack pando status --json`.

Index state and config live under the user cache dir, never inside a repository.

## Acceptance Criteria

- [x] All child stories are done.
- [ ] A fresh machine with `pando` on PATH gets semantic search and impact tiers 2–3 on an opted-in repo by running only `gintrack serve`.
- [ ] Without `pando`, everything still starts and the tiers answer `unavailable` with a reason.
- [ ] ADR-039 moves from `proposed` to `accepted` once the open questions are settled.

## Notes

Open questions left in the ADR: whether instances outlive `serve`, stdio vs loopback HTTP and a stable port, pinning the embedding model, `agui-serve`, Windows/macOS process control.
