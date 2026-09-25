---
id: GIT-US-0120
type: story
title: Render a compact token-budgeted impact report
status: backlog
priority: medium
parent: GIT-EP-0025
milestone: GIT-M-0015
author: claude
labels: [server, mcp, agent-ok]
estimate: 3
created: 2026-09-24T12:10:32Z
updated: 2026-09-24T12:10:32Z
links:
  - { kind: blocked_by, target: GIT-US-0119 }
---

## Description

As an agent, I want the impact result as a compact report within a token budget, so asking "what does this PR affect?" costs a few hundred tokens instead of a spec folder.

## Acceptance Criteria

- [ ] A renderer produces JSON and a terse text form: one line per requirement (ref, title, tier, status, suspect, short reason), ordered failing/suspect first, then tier.
- [ ] A `budget` parameter (tokens, default 1500) truncates lowest-priority hits and reports `truncated: n` with a cursor to fetch the rest.
- [ ] Golden tests assert the typical-PR fixture stays ≤ 1.5k tokens (estimated by a documented tokenizer approximation).
- [ ] Shared by MCP, CLI and HTTP; docs/08 shows an example.

## Notes

Success criterion of the milestone.
