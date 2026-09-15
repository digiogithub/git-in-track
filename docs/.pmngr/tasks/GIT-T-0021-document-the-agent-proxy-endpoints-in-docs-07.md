---
id: GIT-T-0021
type: task
title: Document the agent proxy endpoints in docs/07
status: in_review
priority: medium
parent: GIT-US-0049
milestone: GIT-M-0013
author: mcp
labels: [docs, agent-ok]
estimate: 1
created: 2026-09-13T13:15:48Z
updated: 2026-09-15T15:40:07Z
started: 2026-09-15T15:28:34Z
---

## Description

Document `GET /api/v1/agent/info` and `POST /api/v1/agent/run` in `docs/07-cli-and-api.md`: request and response shapes, the SSE framing (bare `data: {json}` frames whose discriminator is the JSON `type` field), the error problem codes, the `agent.pando` configuration block with `GINTRACK_PANDO_TOKEN`, the `--agent` flag and the `features.agent` capability. State explicitly that the Pando token stays server-side and that the feature is companion-only. Add a `CHANGELOG.md` entry.

## Acceptance Criteria

- [ ] Both endpoints, the config block, the env var, the flag and the capability are documented in one coherent section.
- [ ] The security note about token injection and companion-only availability is present.
- [ ] `CHANGELOG.md` records the feature under the unreleased section.
