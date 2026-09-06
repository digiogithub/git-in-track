# ADR-027 — cloudflared is embedded as a library, and a quick tunnel is a sharing convenience

- **Status:** Accepted
- **Date:** 2026-09-06
- **Phase:** 2 (Companion CLI, `GIT-EP-0003`)
- **Related:** [ADR-005](ADR-005-companion-cli-go-embed.md), [ADR-025](ADR-025-the-cors-proxy-security-model.md)
- **Implements:** `GIT-US-0043` — Share a running companion over a public URL

## Context

`gintrack serve` binds `127.0.0.1:7317` and is reachable from one machine. That
is the right default and it is the whole of the security model (ADR-005): the
bearer token guards the API, the loopback bind guards everything else. It is also
the reason two ordinary requests have no answer today — showing a board to
someone in a call, and opening the same workspace from a phone on another
network. Both need an address that exists outside the machine, and every way to
get one is worse than it sounds: a port forward on the router, a public bind with
`--allow-remote`, a reverse proxy and a certificate, or a third-party account.

A Cloudflare **quick tunnel** removes all of that. A `POST` to
`https://api.trycloudflare.com/tunnel` with an empty body returns credentials and
a `<random-words>.trycloudflare.com` hostname; the process then holds outbound
connections to the Cloudflare edge and the edge forwards requests to a local
address. There is no account, no DNS record, no inbound port and no certificate
to manage.

The question this ADR settles is not whether to use that service. It is **how the
companion speaks to it**, and **what a tunnel is allowed to mean** in a product
whose server has write access to the user's repositories.

Two facts shape the answer.

**The origin is not a static site.** Whatever is published is the companion:
every `/api/v1` route, the write routes included, over every mounted repository.
A tunnel is therefore not a display feature with a security note attached; it is
a security decision with a display feature attached.

**cloudflared is not a small dependency.** It is the transport for a global
network, and the module graph says so. Measured on the real `gintrack` binary,
built the way a release is (`-trimpath -ldflags "-s -w"`), embedding it took the
binary from **54.4 MB to 68.3 MB** — **+13.9 MB, a quarter again as large** — and
added around **60 indirect modules**: `quic-go`, the full
Prometheus client stack, seven OpenTelemetry modules with their gRPC exporter,
`sentry-go`, `gopsutil` and `urfave/cli` among them.

## Decision

**The companion links cloudflared as a Go library, in `internal/tunnel`, and a
quick tunnel is an explicitly enabled, temporary sharing convenience — never a
deployment.**

**1. The library, and nothing under `cloudflared/cmd/`.** `internal/tunnel`
imports `connection`, `supervisor`, `orchestration`, `ingress`, `features`,
`tlsconfig` and their siblings, and reimplements what `cloudflared tunnel --url`
does with them. The `cmd/` tree is off limits by rule, not by taste: it calls
`sentry.Init`, installs process-global signal handlers, starts an auto-updater
and writes pidfiles. Every one of those is a process-wide side effect a
long-running server cannot accept from a feature the user may never turn on.

**2. A `Manager` that can be started and stopped repeatedly.** The UI toggle
demands it, and cloudflared does not offer it: `connection.NewObserver` starts a
dispatch goroutine that cannot be stopped, and `supervisor.NewSupervisor`
registers Prometheus collectors with `MustRegister` against
`prometheus.DefaultRegisterer`, so a second tunnel in one process panics. The
manager creates exactly one observer for its lifetime and separates rounds with a
generation counter, so a late event from a stopped tunnel cannot be applied to
the next one; and it swaps `prometheus.DefaultRegisterer` for a throwaway
registry across the few microseconds construction takes, under a mutex. Both are
worked around at the seam rather than in a fork.

A third defect cannot be worked around at all, so it is recorded here instead.
`ingress/origins.(*resolver).peekDial` writes `r.network` and `r.address` with no
synchronization, and Go's resolver calls `Dial` from several goroutines at once
whenever it queries more than one nameserver. Any tunnel that resolves a name
therefore trips the race detector inside cloudflared. Our own code is clean under
`-race`; the consequence is that the live end-to-end test skips when the detector
is compiled in, and that no test which opens a real tunnel can run under `-race`
until upstream fixes it. Tests that exercise the server's tunnel routes use an
injected fake driver for exactly this reason.

**3. The state machine tells the truth about reachability.** `Start` returns as
soon as the broker hands over the hostname, with state `starting` and the URL
already populated. The URL exists before DNS has propagated and before any edge
connection is up; only an edge connection moves the state to `connected`. The
API and the UI carry that distinction rather than smoothing it over, because a
link shared during `starting` fails for the person who opens it.

**4. The hostname is regenerated on every enable.** The broker mints a new
anonymous tunnel each time, so nothing survives a toggle: no stored hostname, no
"reconnect to the previous address", no cache. Turning the tunnel off invalidates
every link that was shared, which is the only revocation this service offers and
is therefore treated as a feature.

**5. A tunnel over an unauthenticated companion is refused, at every entrance.**
`POST /api/v1/tunnel` answers `409` with `tunnel_requires_token`; `gintrack serve
--tunnel --token none` exits before anything listens; and `server.New` refuses
the same combination on its own, which is what covers the configuration-driven
path. The token is the only thing between a stranger and the user's working
copies, and publishing a server that has none is not a decision the product
offers — so it is a refusal in three places rather than a check the UI is trusted
to make.

**6. It is off by default, always an explicit act, and never persisted by
accident.** The tunnel opens from the `--tunnel` flag, from
`server.tunnel.enabled` at startup, or from the settings card — never on its own
and never as a consequence of some other setting. A toggle made over the API is
deliberately *not* written back to the configuration file: it lasts for the life
of the process, because an API toggle that persisted would republish the
workspace on the next `serve` with nobody watching. Shutdown closes the tunnel,
so no published workspace outlives the server. `features.tunnel` in `GET
/api/v1/capabilities` lets a build or a runtime that cannot tunnel answer
honestly, and the card disappears rather than offering a switch that would do
nothing.

## Consequences

**Good.** Sharing a board or opening the workspace from a phone takes one switch,
with no account, no router configuration, no certificate and no third party
holding a credential of the user's. The tunnel is HTTPS end to end from the
browser to the Cloudflare edge. Because the transport is a library, the tunnel's
lifecycle belongs to the process that owns the server: it goes down with
`Ctrl+C`, it cannot outlive a crash as an orphaned child, and it does not depend
on a `cloudflared` binary being installed, on `PATH` and of a compatible version.
The state is observed from cloudflared's own connection events rather than parsed
out of a log stream.

**Bad, and unavoidable.**

- **The binary grows by 13.9 MB** — 54.4 MB to 68.3 MB, stripped — for
  every user, including the ones who never open a tunnel. Go links what is
  imported, and the tunnel is compiled in unconditionally so that
  `features.tunnel` can be answered without a build tag.
- **Around 60 new indirect modules** enter the graph, and with them their CVE
  feed, their release cadence and their own transitive updates. A dependency
  audit of this project is now substantially a dependency audit of cloudflared.
- **`sentry-go` is linked but never initialised.** `sentry.Init` is called only
  under cloudflared's `cmd/` tree, which this module never imports, so no DSN is
  ever configured and the SDK has nowhere to send anything: there is no telemetry
  egress from it. It is dead weight in the binary, and it is weight a reader will
  find with `go mod graph` and reasonably ask about — hence this paragraph.
- **The dependency is pinned to a pseudo-version.** cloudflared publishes no
  semver tags at all, so `go.mod` carries
  `v0.0.0-20260903222438-2253eeeb25a4`. Upgrading means picking a commit and
  re-testing against a live tunnel; there is no release note to read and no
  compatibility promise to lean on. cloudflared exposes no injection point for
  either of the two workarounds above, so an upgrade can break them silently and
  the package's tests are what has to catch it.
- **It forces `go 1.26` on this module**, because cloudflared's own `go.mod`
  declares it. Everyone building git-in-track now needs a Go 1.26 toolchain, and
  CI's `GO_VERSION` moved with it. This is the single largest cost of the
  decision that is paid by people who will never use the feature.

**What a quick tunnel is not.** Cloudflare offers **no uptime guarantee** on
`trycloudflare.com` and its own disclaimer reserves the right to investigate
usage of the service. A quick tunnel is therefore a sharing convenience with a
lifetime measured in minutes, and never a way to deploy or host anything. The
documentation says so in those words (docs/07 §4.1 and §5.1), the settings card
says so while the tunnel is up, and the hostname's regeneration on every enable
makes the shape of the feature agree with the sentence.

**The exposure is real and is documented as such.** An open tunnel publishes a
server with read *and* write access to every mounted repository, behind one
bearer token. The SPA bundle is served outside the auth group, so anyone with the
URL loads the interface and only `/api/v1` demands the token; and a
`https://<host>/?token=<token>` link is a full credential. Those three sentences
are in docs/07 §5.1 and docs/05, and the settings card repeats them where the
user is deciding.

## Alternatives considered

- **Ship or spawn the `cloudflared` binary.** The obvious answer and the one
  actually asked against: the user asked for a library. It is also the worse
  engineering. Shipping the binary means carrying a second executable per
  platform through GoReleaser, the archives and the Homebrew cask; spawning one
  found on `PATH` means the feature works on some machines and not others,
  parsing a human-readable log to learn the URL, and owning a child process that
  can outlive a crash and keep a tunnel open with nobody watching it. The library
  costs binary size, which is a cost paid once at build time and visible in the
  release notes.
- **Import cloudflared's `cmd/` tree and call its `tunnel --url` path.** Far less
  code here — `internal/tunnel` exists precisely because this was refused. That
  tree calls `sentry.Init`, installs process-global signal handlers, starts an
  auto-updater and writes pidfiles. Any one of them is disqualifying inside a
  long-running server that a user did not start for this feature; together they
  are not negotiable.
- **Fork cloudflared to fix the unstoppable observer goroutine and the
  `MustRegister` collectors.** It would make `internal/tunnel` shorter and
  honest. It also means owning a security-relevant network transport's fork
  forever, against a dependency with no tags. Rejected in favor of two documented
  workarounds at the seam.
- **Bind publicly with `--allow-remote` and document a reverse proxy.** No new
  dependency and no third party. It also asks a user who wanted to show someone a
  board to open a port, obtain a certificate and understand what they exposed —
  and it exposes the same server with none of the "temporary, revocable,
  regenerated on every enable" properties a quick tunnel has.
- **A named Cloudflare tunnel, or another provider's authenticated tunnel.**
  Stable hostnames, an uptime expectation, and access policies in front of the
  origin — genuinely better for anything long-lived. It needs an account, a
  token, DNS and a configuration file, which is a different product decision from
  "publish this for ten minutes". Not excluded later: `provider` is a field on
  the status document, and `server.tunnel` is a section rather than a boolean.
- **Do nothing and keep the companion loopback-only.** The safest option, and it
  stays the default. Rejected as the *only* option because the two requests above
  are real and the answers users reach for on their own — a router port forward,
  a public bind with no token, a stranger's tunnel service — are all worse than a
  toggle that refuses to run without a token and regenerates the address every
  time.
