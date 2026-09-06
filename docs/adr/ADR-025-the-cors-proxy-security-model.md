# ADR-025 — The companion's CORS proxy is a git proxy, not a forwarder

- **Status:** Accepted
- **Date:** 2026-09-06
- **Phase:** 4 (Git sync, `GIT-EP-0005`)
- **Related:** [ADR-005](ADR-005-companion-cli-go-embed.md), [ADR-006](ADR-006-isomorphic-git-vs-go-git.md)
- **Implements:** `GIT-US-0042` — Serve the CORS proxy the docs promise

## Context

Browser-only mode runs git in the tab over `isomorphic-git`. Git hosts answer
the smart-HTTP endpoints — `info/refs`, `git-upload-pack`, `git-receive-pack` —
without any `Access-Control-Allow-Origin`, so the browser refuses the response
before the app sees a byte. A proxy that adds those headers is the only way for
browser-only mode to fetch or push at all.

`docs/06-git-sync.md` §6.3 has always said the companion serves one at
`http://127.0.0.1:7317/cors-proxy/`. It never did. The obvious fix — mount a
reverse proxy at that path — is also the dangerous one, because such an endpoint
turns the user's own machine into a request forwarder that any web page in that
browser can reach. On loopback it can also reach things the open internet cannot:
other services on `127.0.0.1`, the LAN, and the cloud metadata endpoint at
`169.254.169.254`. That is the classic server-side request forgery shape, with
the twist that the attacker does not even need a vulnerable parameter — the
endpoint's whole purpose is to take a URL and fetch it.

The alternative of not building it was considered seriously. It was rejected
because §6.3's own recommendation — self-host `@isomorphic-git/cors-proxy` — has
exactly the same exposure with none of the constraints below, and because a
public proxy sees the user's repository traffic and any token sent with it. The
safest proxy available to a user who already runs the companion is the one on
their own machine, *provided it is not a general-purpose forwarder*.

## Decision

The endpoint is built, and it is specified as a series of refusals. Forwarding is
the last thing that happens, only when every one of them has passed.

**1. The caller is authenticated, twice.** The request must carry an `Origin` the
companion already trusts — the loopback origins of its own port, the Vite dev
server in development, and whatever `--allow-origin` added — and this run's
bearer token. A request with no `Origin` at all is refused: only a browser has
business here, and browsers always send one on a cross-origin fetch.

**2. The token travels in `X-Gintrack-Token`, never in `Authorization`.** Two
credentials are in flight and they belong to different parties: the companion's
token authenticates the tab to the companion, and the git host's credential is
what `isomorphic-git` puts in `Authorization`. Keeping them in separate headers
is what makes "forward one, strip the other" expressible at all. The custom
header has a second effect: it forces a CORS preflight, so a drive-by page cannot
even reach the handler without first being allowed by origin.

**3. Only three paths, with their own methods.** `GET /info/refs` with
`?service=git-upload-pack` or `?service=git-receive-pack`, `POST
/git-upload-pack`, `POST /git-receive-pack`. Everything else is refused,
including the dumb protocol. The worst a caller who gets past every other check
can do is speak the git wire protocol.

**4. Only allow-listed hosts, and the allow-list is derived, not typed.** The
hosts of the remotes of the registered repositories are on it, plus anything in
`git.corsProxy.allowedHosts`. This is what makes the endpoint work with no
configuration and still not be a forwarder: a caller can only reach a host the
user already keeps a repository on. Entries are `host` or `host:port` compared
for equality — never patterns, because a pattern turns an allow-list into a
guess. The scheme upstream is always `https`.

**5. The address is validated, and the validated address is the one dialed.**
Checking the hostname is not enough: a name that passes the check can resolve to
`127.0.0.1` by the time the connection is made. So the proxy's dialer resolves
the name itself, exactly once, refuses the whole attempt unless every address is
a public unicast address, and then connects to one of those addresses. There is
no second lookup for a rebinding attack to poison. Refused: loopback,
unspecified, private (RFC 1918 and IPv6 ULA), link-local — which is what covers
`169.254.169.254` — multicast, carrier-grade NAT (`100.64.0.0/10`),
`192.0.0.0/24`, benchmarking (`198.18.0.0/15`) and `240.0.0.0/4`.

**6. Bounded in size and time.** 32 MiB of request body, 256 MiB of response,
120 s for the exchange. The proxy route is exempt from the API's 30-second
deadline because a pack transfer is not a request that must finish quickly, and
carries its own instead.

**7. Redirects are re-validated, not followed.** Each hop is put through the
scheme, host allow-list and path rules again, at most three times. A host that
answers a redirect into an internal service gets the same refusal the original
request would have. A redirect that changes host drops `Authorization`: the
browser aimed that credential at one host.

**8. Headers pass through allow-lists in both directions.** Upstream:
`Accept`, `Accept-Language`, `Authorization`, `Content-Type`, `Git-Protocol`, and
a fixed `User-Agent`. The browser's own `User-Agent`, `Origin`, `Referer`,
`Cookie`, the forwarding headers and the companion's token are all absent by
construction. Downstream: `Cache-Control`, `Content-Type`, `Expires`, `Pragma`,
`WWW-Authenticate`. `Set-Cookie` cannot reach the tab.

The endpoint can be switched off entirely with `git.corsProxy.enabled: false`,
and the allow-list derivation with `git.corsProxy.allowRepoRemotes: false`.

## Consequences

**Good.** The setup §6.3 has promised for two phases works, with no
infrastructure and no third party seeing the user's repository traffic. The
browser app adopts the companion's proxy the moment it detects one, so the common
case needs no configuration. The refusals are testable as refusals, and are
tested that way: each has a case in `internal/server/cors_proxy_test.go` that
asserts the upstream was never reached, not merely that a status code came back.

**Bad.** The allow-list is derived from remotes read off disk, so a repository
whose remote was added after the companion started is only reachable after the
60-second cache expires. A host reached over plain HTTP, or a self-hosted git
server on the LAN, cannot be proxied at all — the private-address rule refuses
it, deliberately, and the answer for that setup is to run the companion's own
sync rather than browser-only mode. The response is streamed with an allow-listed
subset of headers, so a host that needs something outside that set will not work
through this proxy; adding one is a code change, which is the intent.

**Not a boundary against the local user.** As with the rest of the companion
(ADR-005), the token is not a defense against another process running as the same
user on the same machine. It is a defense against the browser, which is the
threat this endpoint actually adds.
