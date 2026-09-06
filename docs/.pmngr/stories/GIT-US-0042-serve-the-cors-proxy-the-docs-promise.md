---
id: GIT-US-0042
type: story
title: Serve the CORS proxy the docs promise
status: in_review
priority: high
parent: GIT-EP-0005
milestone: GIT-M-0005
author: team
labels: [server, web]
estimate: 8
created: 2026-09-06
updated: 2026-09-06
---

## Description

As someone running git-in-track in browser-only mode, I want to fetch and push
without standing up infrastructure of my own, so that the "browser UI plus local
companion for networking" setup `docs/06-git-sync.md` §6.3 describes actually
works.

§6.3 made two claims that were false when this story opened:

1. it referred to "the reverse-proxy snippet **we ship in the docs**" — an
   nginx/Caddy configuration forwarding only `/*/info/refs`,
   `/*/git-upload-pack` and `/*/git-receive-pack` to an allowlisted set of hosts.
   No such snippet existed anywhere in the repository;
2. it said "the companion also serves one at `http://127.0.0.1:7317/cors-proxy/`
   when running". There was no such route in `internal/server`; `grep` found no
   implementation at all.

Git hosts send no `Access-Control-Allow-Origin` on the smart-HTTP endpoints, so
without a proxy a tab cannot fetch or push at all. A user following §6.3 reached
a dead end, and this was the criterion holding `GIT-EP-0005` and `GIT-M-0005`
open.

**Security is the story, not a section of it.** A forwarder running on the user's
own machine, reachable from any page that browser has open, is a server-side
request forgery hole unless every step is a refusal. The design is written up in
[ADR-025](../../adr/ADR-025-the-cors-proxy-security-model.md); the short form is
that the endpoint authenticates its caller, forwards only three paths, only to
hosts the user's own repositories already use, only to public unicast addresses
it resolved itself, under size and time limits, re-validating every redirect, and
copying headers through allow-lists in both directions.

## Acceptance Criteria

- [x] The companion serves the proxy at `http://127.0.0.1:7317/cors-proxy/`,
      forwarding only `/info/refs` (with `?service=git-upload-pack` or
      `git-receive-pack`), `/git-upload-pack` and `/git-receive-pack`, and adding
      the CORS headers a browser needs.
- [x] Every request needs the run's bearer token in `X-Gintrack-Token` and an
      `Origin` the companion already trusts. `Authorization` is left free for the
      git host's own credential and is forwarded to it; the companion's token
      never leaves the machine.
- [x] The target host must be a remote of a registered repository or listed in
      `git.corsProxy.allowedHosts`. Everything else is refused.
- [x] The name is resolved once, every address it yields is checked, and the
      connection is made to one of those exact addresses, so loopback, private,
      link-local, CGNAT and reserved ranges are unreachable even through DNS
      rebinding.
- [x] Request and response bodies are bounded, the exchange has a deadline, and a
      redirect is re-validated against every rule before it is followed.
- [x] Table-driven Go tests cover the refusals: a blocked host, a private or
      loopback target, a caller with no token, a foreign origin, an oversized
      body, a redirect to a disallowed host and a non-git path.
- [x] The nginx and Caddy recipe §6.3 claimed to ship is in §6.3, with the same
      allow-list discipline.
- [x] Browser-only mode adopts the companion's proxy automatically when the
      companion is present, keeps the per-workspace configuration, validates a
      configured proxy with the `info/refs` preflight §6.3 promises, and never
      routes traffic through a third-party proxy the user did not type.
- [x] `docs/06` §6.3, `docs/07` (the route, its error codes and the configuration
      keys) and `docs/02` describe what was built.

## Notes

`internal/core` is untouched: the proxy is native server code and the WASM build
never sees it.

The one thing §6.3 described and this story deliberately did **not** build is the
mount-wizard probe for "self-hosted git servers that already send permissive CORS
headers". §6.3 now states it as a manual step — leave the proxy field empty and
sync; a host that already sends the headers works — rather than as an automatic
probe, because a probe that silently decides no proxy is needed is a probe that
can silently be wrong about where a credential goes.
