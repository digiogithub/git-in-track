---
id: GIT-T-0118
type: task
title: Verify the end-to-end setup against a live Pando instance
status: todo
priority: medium
parent: GIT-US-0069
milestone: GIT-M-0013
author: mcp
labels: [docs, ci]
estimate: 3
created: 2026-09-13T13:18:11Z
updated: 2026-09-13T13:18:11Z
---

## Description

Follow the generated configuration against a real `pando agui-serve --no-tls` on loopback plus `gintrack serve --agent --mcp-http`, and confirm that a chat turn reaches the agent, that the gintrack MCP tools appear in its toolset, and that a question answered from the backlog returns real item ids. Record the exact commands and the observed output in the documentation, and fix whatever the template got wrong. Add a `CHANGELOG.md` entry for the feature.

## Acceptance Criteria

- [ ] A documented sequence of commands produces a working chat that can answer a question about the backlog.
- [ ] The gintrack MCP tools are confirmed present in the agent's toolset and the evidence is recorded.
- [ ] Any template or documentation error found is fixed in the same change, and `CHANGELOG.md` is updated.
