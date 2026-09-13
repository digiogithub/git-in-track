---
id: GIT-T-0151
type: task
title: Extend GET and PATCH /api/v1/sync/settings with the engine knobs
status: todo
priority: medium
parent: GIT-US-0078
milestone: GIT-M-0011
author: mcp
labels: [server, agent-ok]
estimate: 3
created: 2026-09-13T13:19:00Z
updated: 2026-09-13T13:19:00Z
---

## Description

Extend the existing settings handler (`internal/server/sync.go:379`) with workers, batch size, rate limit and retry policy, validating ranges and applying the change to the running engine, not only to the stored config. Persist with the reload-mutate-save pattern of `gitState.persist` (`internal/server/git.go:336-352`) and return `persisted bool` as `handleGitSettingsPatch` does (`:374-393`).

## Acceptance Criteria

- [ ] All four knobs are readable and writable, with out-of-range values rejected by a problem document.
- [ ] A PATCH changes the behaviour of the running engine without a restart, proved by a test.
- [ ] `persisted` is false when the server has no config path, matching the git settings contract.
- [ ] `go test -race ./internal/server/...` covers validation, live application and persistence.
