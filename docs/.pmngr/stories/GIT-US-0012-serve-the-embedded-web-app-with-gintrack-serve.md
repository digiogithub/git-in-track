---
id: GIT-US-0012
type: story
title: Serve the embedded web app with `gintrack serve`
status: done
priority: critical
parent: GIT-EP-0003
milestone: GIT-M-0003
author: team
labels: [cli, server, security]
estimate: 5
created: 2026-09-03T00:00:00Z
updated: 2026-09-04T05:48:51Z
started: 2026-09-04T05:06:40Z
closed: 2026-09-04T05:48:51Z
links:
  - { kind: blocked_by, target: GIT-US-0010 }
---

## Description

As a user who installed the binary, I want a single command that opens the full UI against
my repositories, so that I get native speed and file watching without running a build
toolchain.

`gintrack serve` starts a chi HTTP server bound to `127.0.0.1:7317`, serves the `web/dist`
bundle embedded with `go:embed`, and opens the browser. It generates a random per-run auth
token, writes it to the state directory with mode `0600`, and requires it on every request.
Binding a non-loopback address requires `--allow-remote` and refuses to start without a
token.

Flags: `--addr`, `--root` (repeatable, one per repository), `--open/--no-open`,
`--allow-remote`, `--watch-mode`.

## Acceptance Criteria

- [ ] `gintrack serve` binds `127.0.0.1:7317` by default and serves the embedded UI.
- [ ] The bundle is embedded at build time; the binary needs no external files.
- [ ] A random token per run is written `0600` and required on every request.
- [ ] Requests with a foreign `Origin` are rejected, including WebSocket upgrades.
- [ ] `--allow-remote` is required for any non-loopback bind and prints a warning.
- [ ] `--addr` reports a clear error when the port is already in use.
- [ ] SPA routes fall back to `index.html`; static assets get correct cache headers.
- [ ] A build without the `embed` tag still runs and explains that no UI is bundled.
