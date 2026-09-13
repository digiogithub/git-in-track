---
id: GIT-T-0060
type: task
title: Implement the preview plan and document the vault methods
status: todo
priority: medium
parent: GIT-US-0047
milestone: GIT-M-0011
author: mcp
labels: [core, server, docs, agent-ok]
estimate: 2
created: 2026-09-13T13:16:46Z
updated: 2026-09-13T13:16:46Z
---

## Description

Implement `youtrack.import.preview` as the resolution half of `run` with the writes replaced by a plan: per issue `{youtrackId, title, mappedType, action, targetId, parent, milestone, warnings}`. It must share the resolution code with `run` so the two can never disagree. Then document both methods in `docs/07-cli-and-api.md` §4 alongside the other core API methods and add a `CHANGELOG.md` entry.

## Acceptance Criteria

- [ ] `preview` writes nothing and returns the per-issue plan with the same action decision `run` would take.
- [ ] Resolution code is shared between `preview` and `run`.
- [ ] `docs/07-cli-and-api.md` documents both methods and `CHANGELOG.md` has an entry.
- [ ] `go test -race ./internal/vault/...` asserts that preview and run agree on actions for the same input.
