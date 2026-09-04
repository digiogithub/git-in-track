---
id: GIT-US-0026
type: story
title: Document AGENTS.md conventions for agent contributors
status: backlog
created: 2026-09-03T00:00:00Z
updated: 2026-09-03T00:00:00Z
author: team
priority: high
parent: GIT-EP-0006
milestone: GIT-M-0006
estimate: 3
labels: [docs, mcp, agent-ok]
links:
  - kind: blocked_by
    target: GIT-US-0025
---

## Description

As a maintainer, I want the rules for agent contribution written down in the repository, so
that an agent joining the project knows how to pick up work safely and a human reviewer
knows what to expect.

`AGENTS.md` documents the seven-step loop from docs/11-roadmap.md §6: how to find eligible
stories, how to claim one with a `rev`-carrying update, branch and commit conventions, how
to report progress as a comment on the story, how to open a PR that references the story,
and how to close it. It also states the hard limits: the WIP cap, the human-only areas,
the definition of done, and the fact that every agent PR is reviewed by a human.

The same document explains how to connect an MCP client to `gintrack mcp`.

## Acceptance Criteria

- [ ] `AGENTS.md` exists at the repository root and is linked from the README.
- [ ] The story pick-up loop is documented step by step with tool calls.
- [ ] Branch, commit and PR conventions are stated and match the guidelines.
- [ ] The WIP cap for agent work in progress is stated and enforced by the board.
- [ ] Human-only areas (data model, security, release pipeline, roadmap) are listed.
- [ ] Attribution via `Co-Authored-By:` is specified.
- [ ] MCP client configuration is shown with a working example.
- [ ] An agent completes one real story end to end following only this document.
