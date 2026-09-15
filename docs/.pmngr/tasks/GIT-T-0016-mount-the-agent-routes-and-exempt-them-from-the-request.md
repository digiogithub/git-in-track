---
id: GIT-T-0016
type: task
title: Mount the agent routes and exempt them from the request timeout
status: done
priority: medium
parent: GIT-US-0049
milestone: GIT-M-0013
author: mcp
labels: [server, agent-ok]
estimate: 2
created: 2026-09-13T13:15:37Z
updated: 2026-09-15T16:42:57Z
started: 2026-09-15T15:28:31Z
closed: 2026-09-15T16:42:57Z
---

## Description

Register the group with one `p.Route("/agent", s.mountAgent)` line inside the bearer-auth group in `internal/server/api.go`, next to `/mcp`, `/git` and `/sync`. Add the `/agent/run` path to the exemption list of `s.timeoutExceptStream` (`internal/server/server.go:355`) the way `/events` and the CORS proxy are exempt, so a long conversation turn is not cut off after 30 seconds. Add the `agentState` field to `Server` and construct it in the server constructor from the resolved options.

## Acceptance Criteria

- [ ] Both endpoints require the companion bearer token and return 401 without it.
- [ ] A run lasting longer than `requestTimeout` completes instead of being cut off.
- [ ] `GET /api/v1/agent/info` returns a `not_implemented` or `not_found` problem when no Pando URL is configured.
