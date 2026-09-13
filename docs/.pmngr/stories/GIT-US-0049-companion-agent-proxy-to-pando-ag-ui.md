---
id: GIT-US-0049
type: story
title: Companion agent proxy to Pando AG-UI
status: backlog
priority: high
parent: GIT-EP-0018
milestone: GIT-M-0013
author: mcp
labels: [server, security, docs]
estimate: 8
created: 2026-09-13T13:11:47Z
updated: 2026-09-13T13:11:47Z
---

## Description

As a companion user, I want `gintrack serve` to expose an authenticated `/api/v1/agent` group that relays my chat turns to a local Pando AG-UI adapter, so that the browser can talk to an agent without ever holding the Pando bearer token.

Add `config.Agent{Pando config.Pando}` to `internal/config/config.go` with `url`, `token`, `agent` (default `coder`) and `insecure_tls`, wired through `config.Resolve`/`applyEnv` (`internal/config/load.go:186`) with a `GINTRACK_PANDO_TOKEN` override — a distinct name, because `GINTRACK_TOKEN` already means two other things (report §8.7). Validate in `internal/config/validate.go`. Add a new `internal/server/agent.go` mounted with one line `p.Route("/agent", s.mountAgent)` in `internal/server/api.go:96`, inside the existing `s.bearerAuth` group. `GET /info` proxies Pando `GET /api/v1/agui/info` and rewrites every `agents[].url` to a companion-relative `/api/v1/agent/run` so the browser never learns the Pando origin. `POST /run` streams Pando `POST /api/v1/agui/{agent}` back verbatim as `text/event-stream`.

The proxy is a streaming relay, not a buffering one: set `X-Accel-Buffering: no`, flush every frame, cap the request body at 1 MiB (`maxRequestBody`, `internal/server/api.go:14`), exempt the route from `s.timeoutExceptStream` (`server.go:355`) the way `/events` and `/cors-proxy` are, and pass the request context straight through so a client disconnect cancels the upstream run — deliberately **not** `context.WithoutCancel`. Only loopback/explicitly configured Pando URLs are dialled. Report the feature as `features.agent` in `handleCapabilities` (`internal/server/server.go:411-435`) and add `--agent` to `cmd/gintrack/serve.go` alongside `--mcp-http`.

## Acceptance Criteria

- [ ] `config.Agent.Pando{url, token, agent, insecure_tls}` parses from the YAML config file, is validated, and `GINTRACK_PANDO_TOKEN` overrides the token.
- [ ] `GET /api/v1/agent/info` returns Pando's discovery document with every agent URL rewritten to `/api/v1/agent/run`.
- [ ] `POST /api/v1/agent/run` streams SSE frames through unbuffered, injecting `Authorization: Bearer <pando token>` server-side; the browser never sees that token.
- [ ] The route sits inside `s.bearerAuth` and is exempt from the 30 s request timeout.
- [ ] Closing the client connection cancels the upstream Pando request within one flush cycle.
- [ ] Request bodies above 1 MiB are refused with an `invalid_request` problem document.
- [ ] `features.agent` is true only when a Pando URL is configured and `--agent` was passed; false otherwise.
- [ ] Upstream failures map to RFC 7807 problems (`internal/server/problem.go`), never a half-written SSE stream without a terminal `RUN_ERROR`.
- [ ] `go test -race ./internal/server/...` covers the happy path, the cancel path and the unauthenticated path against an httptest Pando stub.
- [ ] `docs/07-cli-and-api.md` documents the two endpoints, the capability and the `--agent` flag.

## Notes

Existing precedents to copy: `internal/server/cors_proxy.go` (outbound HTTP with an allow-list and bounds), `internal/server/events.go:44` (a long-lived streaming handler outside the timeout), `internal/server/mcp.go:186` (a feature with its own settings sub-route). Pando's contract: `internal/agui/server.go:26-33` (routes), `:47-103` (origin allow-list + bearer), `internal/agui/sse.go:33-47` (headers, bare `data: {json}` frames whose discriminator is the JSON `type`, not the SSE `event:` field), `:13` (8 MiB body cap).

Pando's `agui-serve` defaults to self-signed TLS (`cmd/agui_serve.go:229-241`); `insecure_tls` exists for that case, and the recommended deployment is `--no-tls` on loopback.

Do **not** generalise `internal/server/cors_proxy.go` — ADR-025 argues against it explicitly and its path allow-list forbids anything but the three git smart-HTTP paths. Do not add the Pando token to `GET /api/v1/capabilities`. Do not attempt this in browser-only mode: there is no server to proxy through.
