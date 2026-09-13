---
id: GIT-T-0036
type: task
title: Mount /api/v1/youtrack and add youtrackState
status: todo
priority: medium
parent: GIT-US-0052
milestone: GIT-M-0011
author: mcp
labels: [server, agent-ok]
estimate: 3
created: 2026-09-13T13:16:12Z
updated: 2026-09-13T13:16:12Z
---

## Description

Add `p.Route("/youtrack", s.mountYouTrack)` to `mountAPI` (`internal/server/api.go:22-101`, beside `:94-96`) and create `internal/server/youtrack.go` with a `youtrackState` that owns the resolved config, builds a `youtrack.Client` on demand and is stored on `Server` (`server.go:134-166`) like `git`, `mcp` and `tunnel`. Register the problem codes `youtrack_unauthorized`, `youtrack_unreachable` and `youtrack_not_configured` in `internal/server/problem.go`.

## Acceptance Criteria

- [ ] The subtree is mounted inside the authenticated `api.Group` and returns 401 without a bearer token.
- [ ] The three new problem codes exist and render as RFC 7807 documents.
- [ ] `go test -race ./internal/server/...` covers mounting and the auth requirement.
