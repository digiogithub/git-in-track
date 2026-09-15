---
id: GIT-T-0013
type: task
title: Implement the SSE relay handlers in internal/server/agent.go
status: done
priority: medium
parent: GIT-US-0049
milestone: GIT-M-0013
author: mcp
labels: [server]
estimate: 5
created: 2026-09-13T13:15:32Z
updated: 2026-09-15T16:42:56Z
started: 2026-09-15T15:28:30Z
closed: 2026-09-15T16:42:56Z
---

## Description

Create `internal/server/agent.go` with `mountAgent(r chi.Router)`, `handleAgentInfo` and `handleAgentRun`, plus an `agentState` holding the resolved Pando URL, an `*http.Client` with the configured TLS behaviour, and the injected bearer token. `handleAgentInfo` fetches Pando's `GET /api/v1/agui/info`, decodes it and rewrites every `agents[].url` to `/api/v1/agent/run` before writing it back. `handleAgentRun` copies the capped request body to `POST {pando}/api/v1/agui/{agent}` with `Authorization: Bearer`, then copies the response body through frame by frame, flushing after each write, with `Content-Type: text/event-stream`, `Cache-Control: no-cache` and `X-Accel-Buffering: no`.

Pass `r.Context()` to the upstream request so a client disconnect cancels the run. Map connection and non-2xx upstream results to RFC 7807 problems via `internal/server/problem.go`.

## Acceptance Criteria

- [ ] Frames reach the client as they arrive, verified by a test asserting timing or flush ordering against an httptest SSE stub.
- [ ] Cancelling the client request cancels the upstream request context.
- [ ] A body above `maxRequestBody` and an upstream failure both produce a problem document, not a truncated stream.
- [ ] `go test -race ./internal/server/...` covers info rewriting, streaming, cancel and the error paths.
