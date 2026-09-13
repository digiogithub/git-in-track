---
id: GIT-T-0211
type: task
title: Detect both-changed conflicts and write a conflict page
status: todo
priority: medium
parent: GIT-US-0087
milestone: GIT-M-0011
author: mcp
labels: [server, agent-ok]
estimate: 3
created: 2026-09-13T13:21:01Z
updated: 2026-09-13T13:21:01Z
---

## Description

Add conflict detection comparing the page's `external.synced_at` and recorded rev against the current local rev and the article's `updated`. One side changed means last writer wins in that direction; both changed means the incoming content is written to `<page>.conflict.md`, the original page is left untouched, and a `youtrack.kb.conflict` event is published on the hub. Feedback-block pruning must not count as a local change.

## Acceptance Criteria

- [ ] The four cases (neither, local only, remote only, both) are decided from `synced_at`, the local rev and the article `updated`.
- [ ] Both-changed writes `<page>.conflict.md`, leaves the page untouched and publishes `youtrack.kb.conflict`.
- [ ] Feedback pruning does not register as a local change.
- [ ] `go test -race ./internal/server/...` covers all four cases.
