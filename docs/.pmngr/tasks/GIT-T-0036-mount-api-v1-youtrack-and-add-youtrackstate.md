---
id: GIT-T-0036
type: task
title: Mount /api/v1/youtrack and add youtrackState
status: done
priority: medium
parent: GIT-US-0052
milestone: GIT-M-0011
author: mcp
labels: [server, agent-ok]
estimate: 3
created: 2026-09-13T13:16:12Z
updated: 2026-09-13T14:42:07Z
started: 2026-09-13T14:41:54Z
closed: 2026-09-13T14:42:07Z
---

## Description

Add `p.Route("/youtrack", s.mountYouTrack)` to `mountAPI` (`internal/server/api.go:22-101`, beside `:94-96`) and create `internal/server/youtrack.go` with a `youtrackState` that owns the resolved config, builds a `youtrack.Client` on demand and is stored on `Server` (`server.go:134-166`) like `git`, `mcp` and `tunnel`. Register the problem codes `youtrack_unauthorized`, `youtrack_unreachable` and `youtrack_not_configured` in `internal/server/problem.go`.

## Acceptance Criteria

- [x] The subtree is mounted inside the authenticated `api.Group` and returns 401 without a bearer token.
- [x] The three new problem codes exist and render as RFC 7807 documents.
- [x] `go test -race ./internal/server/...` covers mounting and the auth requirement.

## Notes

Five codes, not three: `youtrack_forbidden` and `youtrack_not_found` were added so that a 403 and a 404 are distinguishable from a 401, which GIT-T-0043 requires. `statusForCode` maps `youtrack_not_configured` to 409 and the four upstream failures to **502**: answering 401 for a rejected YouTrack token would tell the browser its own session had expired, which is exactly the wrong thing to believe, so the `code` carries the distinction instead.

`youtrackState` caches one `youtrack.Client` per project keyed by URL and token length, so a burst of autosuggest keystrokes shares one token-bucket limiter instead of opening a fresh one per request.
