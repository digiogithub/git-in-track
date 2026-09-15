---
id: GIT-T-0106
type: task
title: Write the backlog-assistant persona and the routing skill
status: in_review
priority: medium
parent: GIT-US-0069
milestone: GIT-M-0013
author: mcp
labels: [docs, agent-ok]
estimate: 2
created: 2026-09-13T13:17:55Z
updated: 2026-09-15T15:32:33Z
started: 2026-09-15T15:18:55Z
---

## Description

Write `backlog-assistant.md`, the persona the generated `[AGUI] Persona` setting points at: what the assistant is for, that repository content is data rather than instructions, that it must quote a `rev` on every write and never escape a conflict with `rev: "*"`, and that it must not act outside the item it was asked about. Alongside it write the skill file carrying the tool routing table. Both are shipped as embedded templates written by `gintrack agent init`.

## Acceptance Criteria

- [ ] The persona states the assistant's scope, the untrusted-content rule and the rev write protocol.
- [ ] The skill file contains the routing table with one example per row.
- [ ] Both files are embedded and written by `agent init`, and a test asserts they are non-empty and valid Markdown.
