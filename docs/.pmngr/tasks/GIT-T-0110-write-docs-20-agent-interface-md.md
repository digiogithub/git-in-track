---
id: GIT-T-0110
type: task
title: Write docs/20-agent-interface.md
status: in_review
priority: medium
parent: GIT-US-0069
milestone: GIT-M-0013
author: mcp
labels: [docs]
estimate: 3
created: 2026-09-13T13:18:00Z
updated: 2026-09-15T15:32:34Z
started: 2026-09-15T15:18:56Z
---

## Description

Write `docs/20-agent-interface.md` covering the architecture (browser to companion proxy to Pando AG-UI in one direction, Pando to the companion `/mcp` in the other), the setup procedure end to end, the configuration reference for both sides, the frontend tool list, the security model with its two tokens in two directions and neither in the browser, and a troubleshooting table. State plainly that the AG-UI agent is Pando's full coder agent and that the persona and skill constrain it by convention, not by enforcement. Link it from `README.md` and the docs index.

## Acceptance Criteria

- [ ] The document covers architecture, setup, configuration, tools, security and troubleshooting.
- [ ] The unenforced-constraint caveat about the coder agent's toolset is stated explicitly.
- [ ] It is linked from `README.md` and the docs reading order.
