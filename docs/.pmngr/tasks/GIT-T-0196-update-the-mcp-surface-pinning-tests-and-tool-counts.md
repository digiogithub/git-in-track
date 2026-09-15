---
id: GIT-T-0196
type: task
title: Update the MCP surface-pinning tests and tool counts
status: done
priority: medium
parent: GIT-US-0088
milestone: GIT-M-0013
author: mcp
labels: [mcp, agent-ok]
estimate: 2
created: 2026-09-13T13:20:09Z
updated: 2026-09-15T16:44:02Z
started: 2026-09-15T16:17:43Z
closed: 2026-09-15T16:44:02Z
---

## Description

Add `search_semantic` to the pinned read-tool list in `internal/mcp/tools_test.go` and to `cmd/gintrack/mcp_test.go`, then update every place that states a tool count: the `gintrack mcp` long help, `docs/08-mcp-server.md` §4, `docs/07-cli-and-api.md` §4.9 and the tool list in `AGENTS.md`. While in `docs/07`, fix the existing drift — it lists twelve tools without `create_milestone` and says "six write tools" where there are seven.

## Acceptance Criteria

- [ ] The pinning tests include the new tool and pass.
- [ ] Every stated tool count and tool list in the CLI help and the docs is correct.
- [ ] The pre-existing `docs/07` drift is fixed in the same change.
