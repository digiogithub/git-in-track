---
id: GIT-T-0173
type: task
title: Coalesce rapid pushes per comment path
status: todo
priority: medium
parent: GIT-US-0072
milestone: GIT-M-0011
author: mcp
labels: [server, agent-ok]
estimate: 2
created: 2026-09-13T13:19:29Z
updated: 2026-09-13T13:19:29Z
---

## Description

Debounce and coalesce enqueues on the comment path so a burst of edits produces one push, following the coalescing shape of `internal/gitops/committer.go:157-211` (coalesce key, `arm`, `time.AfterFunc`, `fire`) rather than inventing a new mechanism. Use a fake clock in tests so the debounce is deterministic.

## Acceptance Criteria

- [ ] Repeated writes to the same comment path within the debounce window produce one push job.
- [ ] Writes to different comment paths are not coalesced together.
- [ ] `go test -race ./internal/server/...` verifies coalescing with a fake clock.
