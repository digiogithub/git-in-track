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
updated: 2026-09-13T13:18:05Z
---

## Description

Write the ADR recording the decision to consume AG-UI directly with `@ag-ui/client` and a custom shadcn chat UI, rather than CopilotKit with a Node sidecar or a Go reimplementation of CopilotKit's GraphQL runtime. Record the context, the three alternatives with why each was rejected, and mandatory negative consequences: a new frontend dependency, a hand-maintained event reducer tracking a young protocol, operational coupling to a per-project Pando instance holding an `ipc.lock`, and a feature that exists only in companion mode. Check `docs/adr/` for the next free number before writing — the number in the story title is indicative.

## Acceptance Criteria

- [ ] The ADR follows the template in `docs/adr/README.md` and uses the next free number.
- [ ] All three alternatives and their rejection reasons are recorded.
- [ ] Negative consequences are listed explicitly, as the ADR conventions require.
