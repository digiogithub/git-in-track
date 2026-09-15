---
id: GIT-T-0114
type: task
title: Write ADR-035 for the AG-UI choice
status: todo
priority: medium
parent: GIT-US-0069
milestone: GIT-M-0013
author: mcp
labels: [docs]
estimate: 2
created: 2026-09-13T13:18:05Z
updated: 2026-09-15T14:43:08Z
---

## Description

Write the ADR recording the decision to consume AG-UI directly with `@pando-ai/sdk/agui` (its `PandoAguiClient`, `PandoThread` and HITL helpers) and a custom shadcn chat UI, rather than CopilotKit with a Node sidecar, a Go reimplementation of CopilotKit's GraphQL runtime, or the generic `@ag-ui/client` whose schemas drop Pando's `outcome: interrupt` and reasoning events. Record the context, the four alternatives with why each was rejected, the deployment shape (one `pando agui-serve` per repository, routed by the companion, Origin stripped, token never in the browser), and mandatory negative consequences: a new frontend dependency published by another project, a protocol still young, operational coupling to a per-project Pando instance holding an `ipc.lock`, and a feature that exists only in companion mode. Check `docs/adr/` for the next free number before writing — the number in the task title is indicative.

## Acceptance Criteria

- [ ] The ADR follows the template in `docs/adr/README.md` and uses the next free number.
- [ ] All four alternatives and their rejection reasons are recorded, with a pointer to `docs/research/2026-09-13-pando-gap-sdk-client.md`.
- [ ] Negative consequences are listed explicitly, as the ADR conventions require.
