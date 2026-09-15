---
id: GIT-T-0156
type: task
title: Document the Pando client contract and its caveats
status: in_review
priority: medium
parent: GIT-US-0077
milestone: GIT-M-0013
author: mcp
labels: [docs, agent-ok]
estimate: 1
created: 2026-09-13T13:19:05Z
updated: 2026-09-15T15:37:01Z
started: 2026-09-15T15:20:15Z
---

## Description

Write the package doc comment for `internal/pando` recording why the client speaks MCP rather than REST (Pando has no HTTP search API), why the two search tools are called separately rather than through `hybrid_search_remembrances` (its merge sorts incommensurable scores), and why the transport is restricted to loopback (no authentication on Pando's MCP HTTP transport). Document the `search.pando` configuration block in `docs/07-cli-and-api.md`.

## Acceptance Criteria

- [ ] The package doc records the three rationales with enough detail that a later reader does not re-litigate them.
- [ ] `docs/07-cli-and-api.md` documents the configuration block and its defaults.
