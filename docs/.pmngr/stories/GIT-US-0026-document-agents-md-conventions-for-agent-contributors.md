---
id: GIT-US-0026
type: story
title: Document AGENTS.md conventions for agent contributors
status: in_review
priority: high
parent: GIT-EP-0006
milestone: GIT-M-0006
author: team
labels: [docs, mcp, agent-ok]
estimate: 3
created: 2026-09-03T00:00:00Z
updated: 2026-09-04T00:00:00Z
links:
  - { kind: blocked_by, target: GIT-US-0025 }
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

- [x] `AGENTS.md` exists at the repository root and is linked from the README.
- [x] The story pick-up loop is documented step by step with tool calls.
- [x] Branch, commit and PR conventions are stated and match the guidelines.
- [x] The WIP cap for agent work in progress is stated and enforced by the board.
- [x] Human-only areas (data model, security, release pipeline, roadmap) are listed.
- [x] Attribution via `Co-Authored-By:` is specified.
- [x] MCP client configuration is shown with a working example.
- [ ] An agent completes one real story end to end following only this document.

## Notes

`AGENTS.md` was rewritten against reality: the "planning phase / the scaffold does not
exist yet" claims were false and are gone, the Makefile section now matches the real
targets (including `wasm-smoke` and the `golangci-lint` `go run` fallback), the milestone
file pattern was corrected from `<KEY>-MS-<NNNN>` to `<KEY>-M-<NNNN>`, and `links[]` is
now spelled `{kind, target}` as `internal/core` parses it.

New sections: **Working through the MCP server** (client configuration, the twelve tools,
the seven-step pick-up loop with tool calls, the `rev` write and `stale_revision` retry
protocol from GIT-US-0025, the untrusted-content boundary, the WIP cap, the human-only
areas) and **Branch, commit and PR conventions** with the `Co-Authored-By:` trailer.

The last acceptance criterion — an agent completing a story end to end following only this
document — is an observation to make on the next agent-picked story, not something this
change can assert about itself.
