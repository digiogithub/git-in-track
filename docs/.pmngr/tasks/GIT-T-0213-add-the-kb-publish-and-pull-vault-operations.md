---
id: GIT-T-0213
type: task
title: Add the KB publish and pull vault operations
status: todo
priority: medium
parent: GIT-US-0090
milestone: GIT-M-0011
author: mcp
labels: [core, server, agent-ok]
estimate: 2
created: 2026-09-13T13:21:10Z
updated: 2026-09-13T13:21:10Z
---

## Description

Add `youtrack.kb.publish` and `youtrack.kb.pull` to the vault dispatch table taking `{project, path, recursive}`. They validate the path, check the project link and enqueue the corresponding job, returning the job id rather than blocking. Add workspace routing in `internal/vault/dispatch.go:73` so the companion can address them per project.

## Acceptance Criteria

- [ ] Both methods exist, validate their arguments and check the project link.
- [ ] Each enqueues its job and returns the job id without blocking.
- [ ] Workspace routing resolves the project correctly.
- [ ] `go test -race ./internal/vault/...` covers validation, the unlinked-project error and the enqueue.
