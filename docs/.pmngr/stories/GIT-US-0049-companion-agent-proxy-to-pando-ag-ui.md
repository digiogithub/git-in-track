---
id: GIT-US-0049
type: story
title: Companion agent proxy to Pando AG-UI
status: done
priority: high
parent: GIT-EP-0018
milestone: GIT-M-0013
author: mcp
labels: [server, security, docs]
estimate: 8
created: 2026-09-13T13:11:47Z
updated: 2026-09-15T16:44:39Z
started: 2026-09-15T15:28:37Z
closed: 2026-09-15T16:44:39Z
---

## Description

As a companion user, I want `gintrack serve` to expose an authenticated `/api/v1/agent` group that relays my chat turns to a local Pando AG-UI adapter, so that the browser can talk to an agent without ever holding the Pando bearer token.

Add `config.Agent{Pando config.Pando}` to `internal/config/config.go` with `url`, `token`, `agent` (default `coder`) and `insecure_tls`, wired through `config.Resolve`/`applyEnv` (`internal/config/load.go:186`) with a `GINTRACK_PANDO_TOKEN` override — a distinct name, because `GINTRACK_TOKEN` already means two other things (report §8.7). Validate in `internal/config/validate.go`. Add a new `internal/server/agent.go` mounted with one line `p.Route("/agent", s.mountAgent)` in `internal/server/api.go:96`, inside the existing `s.bearerAuth` group. `GET /info` proxies Pando `GET /api/v1/agui/info` and rewrites every `agents[].url` to a companion-relative `/api/v1/agent/run` so the browser never learns the Pando origin. `POST /run` streams Pando `POST /api/v1/agui/{agent}` back verbatim as `text/event-stream`.

**Routing table, not a single upstream.** The deployment is one `pando agui-serve` process per repository, so the config holds a `projectId -> {url, token}` table and every request selects its upstream by project. There is no shared multi-cwd Pando process and no co-mounted `pando serve --agui-port`.

**Header hygiene.** The proxy strips the browser's `Origin` header before dialling Pando (Pando skips its CORS check when the header is absent, `server.go:48-53`, and its `AllowedOrigins` is deliberately left empty). The Pando token travels only as an `Authorization` header injected server-side — never as a `?token=` query parameter, in either direction, and never in a URL the browser can see.

**Streaming, not buffering.** Set `X-Accel-Buffering: no`, use `httputil.ReverseProxy` with `FlushInterval: -1` (immediate flush per write — the correct setting for SSE, rather than a hand-rolled flush loop), cap the request body at 1 MiB (`maxRequestBody`, `internal/server/api.go:14`), exempt the route from `s.timeoutExceptStream` (`server.go:355`) the way `/events` and `/cors-proxy` are, and pass the request context straight through so a client disconnect cancels the upstream run — deliberately **not** `context.WithoutCancel`.

**Concurrency cap, owned here.** Pando enforces no limit on concurrent AG-UI runs, so the proxy imposes its own: a per-user in-flight cap and a global in-flight cap, both configurable. Over the cap, refuse with an RFC 7807 problem and a `Retry-After` rather than queueing indefinitely.

Only loopback or explicitly configured Pando URLs are dialled. Report the feature as `features.agent` in `handleCapabilities` (`internal/server/server.go:444`) and add `--agent` to `cmd/gintrack/serve.go` alongside `--mcp-http`.

## Acceptance Criteria

- [ ] `config.Agent.Pando{url, token, agent, insecure_tls}` parses from the YAML config file, is validated, and `GINTRACK_PANDO_TOKEN` overrides the token.
- [ ] A `projectId -> {url, token}` routing table resolves the upstream per request across several `agui-serve` processes; an unknown project id is a typed 404, never a fallback to some other project's agent.
- [ ] `GET /api/v1/agent/info` returns Pando's discovery document with every agent URL rewritten to `/api/v1/agent/run`.
- [ ] `POST /api/v1/agent/run` streams SSE frames through unbuffered with `FlushInterval: -1`, injecting `Authorization: Bearer <pando token>` server-side; the browser never sees that token.
- [ ] The `Origin` header is stripped from the upstream request, and no request the proxy makes carries a `?token=` query parameter.
- [ ] The route sits inside `s.bearerAuth` and is exempt from the 30 s request timeout.
- [ ] Runs over the per-user or global in-flight cap are refused with a problem document carrying `Retry-After`; the caps are configurable and the refusal is covered by a test.
- [ ] Closing the client connection cancels the upstream Pando request within one flush cycle.
- [ ] Request bodies above 1 MiB are refused with an `invalid_request` problem document.
- [ ] `features.agent` is true only when a Pando URL is configured and `--agent` was passed; false otherwise.
- [ ] Health is probed with `GET /api/v1/agui/info` on the selected upstream, since `agui-serve` has no dedicated health route today.
- [ ] Upstream failures map to RFC 7807 problems (`internal/server/problem.go`), never a half-written SSE stream without a terminal `RUN_ERROR`.
- [ ] `go test -race ./internal/server/...` covers the happy path, the cancel path, the cap path and the unauthenticated path against an httptest Pando stub.
- [ ] `docs/07-cli-and-api.md` documents the two endpoints, the capability and the `--agent` flag.

## Notes

Existing precedents to copy: `internal/server/cors_proxy.go` (outbound HTTP with an allow-list and bounds), `internal/server/events.go:44` (a long-lived streaming handler outside the timeout), `internal/server/mcp.go:186` (a feature with its own settings sub-route). Pando's contract: `internal/agui/server.go:26-33` (routes), `:47-103` (origin allow-list + bearer), `internal/agui/sse.go:33-47` (headers, bare `data: {json}` frames whose discriminator is the JSON `type`, not the SSE `event:` field), `:13` (8 MiB body cap).

Pando's `agui-serve` defaults to self-signed TLS (`cmd/agui_serve.go:229-241`); `insecure_tls` exists for that case, and the recommended deployment is `--no-tls` on loopback.

**Pando dependencies (plain ids):** PANDO-EP-0004 adds a real health/readiness endpoint for embedded deployments; until it lands, use `GET /api/v1/agui/info` as the probe and swap it when the healthz route exists. PANDO-EP-0003 (run durability) is what will eventually let a disconnected run survive; today the proxy's cancel-on-disconnect behaviour is correct and deliberate.

Do **not** generalise `internal/server/cors_proxy.go` — ADR-025 argues against it explicitly and its path allow-list forbids anything but the three git smart-HTTP paths. Do not add the Pando token to `GET /api/v1/capabilities`. Do not forward the browser `Origin` and do not ask Pando to populate `AllowedOrigins` as a second defence — it is easy to misconfigure and buys nothing for a server-to-server hop. Do not attempt this in browser-only mode: there is no server to proxy through.
