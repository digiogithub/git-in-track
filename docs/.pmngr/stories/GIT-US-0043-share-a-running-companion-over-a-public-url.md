---
id: GIT-US-0043
type: story
title: Share a running companion over a public URL
status: done
priority: medium
parent: GIT-EP-0003
milestone: GIT-M-0003
author: team
labels: [server, cli, web, security]
estimate: 8
created: 2026-09-06T00:00:00Z
updated: 2026-09-07T07:11:53Z
closed: 2026-09-07T07:11:53Z
---

## Description

As someone running `gintrack serve`, I want to publish the companion on a
temporary public address, so that I can show a board to somebody in a call or
open the same workspace from a phone on another network without forwarding a
port, binding publicly, obtaining a certificate or signing up for anything.

`gintrack serve` binds `127.0.0.1:7317` and is reachable from one machine. That
stays the default and it is the whole security model (ADR-005). The answers a
user reaches for on their own — a router port forward, `--allow-remote` with no
token, a stranger's tunnel service — are all worse than a switch the product
controls.

A Cloudflare **quick tunnel** needs no account, no DNS record and no inbound
port: one anonymous `POST` returns credentials and a `*.trycloudflare.com`
hostname, and the process holds outbound connections to the edge. cloudflared is
linked as a **library** (`internal/tunnel`), never spawned as a binary and never
through its `cmd/` tree, which calls `sentry.Init`, installs process-global
signal handlers, starts an auto-updater and writes pidfiles.

**Security is the story, not a section of it.** What a tunnel publishes is this
server, which has read *and* write access to every mounted repository, guarded by
one bearer token; the SPA is served without authentication, so anyone with the
URL loads the interface. The decision and its costs — roughly 20 MB of binary,
about 60 indirect modules, a pseudo-version pin and the forced `go 1.26` — are
recorded in [ADR-027](../../adr/ADR-027-cloudflared-as-a-library-for-quick-tunnels.md).

## Acceptance Criteria

- [x] `internal/tunnel` drives cloudflared as a library and imports nothing under
      `cloudflared/cmd/`, so no Sentry initialization, signal handler,
      auto-updater or pidfile enters the process.
- [x] A `Manager` can be started and stopped repeatedly in one long-running
      process: exactly one `connection.Observer` for its lifetime, a generation
      stamp that drops events from a previous round, and a throwaway
      `prometheus.DefaultRegisterer` around `supervisor.NewSupervisor` so a
      second tunnel does not panic on a duplicate collector.
- [x] `Start` returns as soon as the hostname is known, with `state: starting`
      and the URL already populated; only an edge connection moves the state to
      `connected`.
- [x] A new hostname is minted on every enable, and nothing about a tunnel is
      cached or persisted across a toggle.
- [x] `GET|POST|DELETE /api/v1/tunnel` serve the status document
      (`supported`, `provider`, `state`, `url`, `connections`, `since`, `error`,
      `tokenConfigured`); `POST` answers `202` and `DELETE` answers `200` with
      the status back at `off`. The token itself appears in no response, no log
      line and no event payload.
- [x] The refusal is enforced at every entrance: `POST /api/v1/tunnel` answers
      `409` with `tunnel_requires_token`, `serve --tunnel --token none` exits
      before anything listens, and `server.New` refuses the same combination for
      the configuration-driven path.
- [x] A toggle made over the API is not written back to the configuration file,
      so a tunnel opened from the web UI cannot republish the workspace on the
      next `gintrack serve`.
- [x] A state change is broadcast on the WebSocket stream as `tunnel.changed`
      carrying the same document, and `GET /api/v1/capabilities` reports
      `features.tunnel`.
- [x] `gintrack serve --tunnel` opens the tunnel once the listener has an
      address and prints the bare public URL — never a `?token=` link — and
      `server.tunnel` configures the same thing from the config file. A tunnel
      that fails to open degrades the run to loopback only instead of killing
      it, and shutdown closes the tunnel.
- [x] The settings card is companion-only, hidden when `supported` is false,
      opens nothing on its own, carries a standing notice while the tunnel is up,
      shows the address as propagating while the state is `starting`, and keeps
      the bare URL and the token-carrying link as two separate controls with the
      "this link is a credential" warning on the second.
- [x] A refusal over an unauthenticated companion is rendered as an explanation
      with the fix, not as a retryable error.
- [x] The docs describe what was built: docs/07 §4.1 (the flag), §3.2
      (`server.tunnel`), §5.1.1 (the security posture), §5.5 (the three routes)
      and §5.6 (`tunnel.changed`); docs/05 §3.1 (the card and its states);
      ADR-027 with the trade-offs; the CHANGELOG entry including the Go 1.26
      requirement.
- [x] Every place the docs claimed Go 1.25+ says 1.26+ (AGENTS.md, docs/07 §2.2,
      docs/09, docs/10 §9).

## Notes

`internal/core` is untouched: the tunnel is native server code and the WASM build
never sees it.

Two workarounds in `internal/tunnel` exist only because cloudflared offers no
injection point, and an upgrade can break either one silently: the single
long-lived observer (its dispatch goroutine cannot be stopped) and the
`prometheus.DefaultRegisterer` swap (`supervisor.NewSupervisor` hardcodes
`MustRegister` against the default registerer). The package's tests are what has
to catch that, since the dependency has no tags to read release notes from.

A named Cloudflare tunnel — stable hostname, an uptime expectation, access
policies — is deliberately out of scope and not excluded later: `provider` is a
field on the status document and `server.tunnel` is a section rather than a
boolean.
