# ADR-041 — Protected quick tunnels put Cloudflare Access OTP in front of the token

- **Status:** Accepted
- **Date:** 2026-10-27
- **Phase:** Post-1.0 (Companion CLI, `GIT-EP-0003`)
- **Related:** [ADR-027](ADR-027-cloudflared-as-a-library-for-quick-tunnels.md) (supersedes the last bullet of its 2026-10-27 amendment only), [ADR-005](ADR-005-companion-cli-go-embed.md)

## Context

ADR-027 made the quick tunnel a sharing convenience whose only gate is the
bearer token. That was the whole security model because it was the only one the
service offered: an anonymous `POST https://api.trycloudflare.com/tunnel`
returned a hostname that anyone could reach, and the companion's SPA bundle is
served outside the auth group, so anyone holding the URL loaded the interface
and faced the token alone (docs/07 §5.1.1).

Between September and October 2026 Cloudflare added a **protected** quick
tunnel (cloudflared 2026.9–2026.10). Provisioning with the body
`{"auth_mode":"otp"}` registers the hostname with Cloudflare Access, and the
connector runs a request authorizer — the importable `quicktunnelauth` package —
*in front of ingress*: a visitor without a session is redirected to a Cloudflare
login that mails a one-time PIN, but only to an address on a recipient list the
connector holds (`cloudflared tunnel --allowed-mail a@b.com,*@corp.com`). Writes
without a session get `401`. The session is a cookie issued and validated in the
connector's own process, stripped before a request is forwarded.

ADR-027's amendment recorded the upgrade to that library and explicitly left
protected mode out: putting a second gate in front of the token changes the
security model ADR-027 defines, so it deserved its own decision. The maintainer
has now asked for it. This ADR is that decision.

## Decision

**A non-empty `server.tunnel.allowedEmails` makes every tunnel the companion
opens a protected quick tunnel, and Cloudflare Access one-time PIN becomes a
first gate in front of the bearer token — never a replacement for it.**

**1. Opt-in, by configuration.** `server.tunnel.allowedEmails` (a list of exact
addresses or `*@domain` rules, each entry optionally comma-separated) or
`gintrack serve --tunnel-allow-email`, repeatable and comma-separated, which
replaces the configured list for one run. Empty — the default — is the public
tunnel of ADR-027, byte for byte: the provisioning body stays empty and no
authorizer is installed. The list applies to every tunnel the process opens,
whether from `--tunnel`, `server.tunnel.enabled` or the settings card, and
there is no route to edit it at runtime: who may pass the gate is decided by
whoever starts the process, exactly like the token.

**2. The token stays mandatory.** Every refusal of ADR-027 §5 holds unchanged
for a protected tunnel: `gintrack serve --tunnel --tunnel-allow-email … --token
none` exits before anything listens, `server.New` refuses the same combination
and `POST /api/v1/tunnel` answers `409 tunnel_requires_token`. The two gates
protect different things and fail differently. The PIN keeps strangers from
reaching the companion at all — not the API, not even the HTML of the web app —
which closes the "anyone with the URL loads the interface" exposure of ADR-027.
The token still decides what someone who is through may do. A leaked `?token=`
link is no longer enough on its own, and a mailbox on the list is not enough on
its own either.

**3. cloudflared's rules, not ours.** The list is validated with
`quicktunnelauth.NewQuickTunnelAuthRecipientPolicy` — the same constructor
`cloudflared --allowed-mail` uses, with its limits (100 entries, 320 bytes
each) — in `config.Validate`, in `server.New` and in `serve` for the flag. A
malformed list stops the server at startup whether or not the tunnel autostarts,
because the list also governs a tunnel opened later from the UI and a typo must
not surface as a failed toggle hours later. A manager handed an invalid list
refuses to start rather than falling back to a public tunnel.

**4. The upstream flow, reassembled without `cmd/`.** `internal/tunnel` mirrors
cloudflared's `RunQuickTunnel` with the importable pieces only, as ADR-027
requires: the `{"auth_mode":"otp"}` body, then per round a state manager bound to
the provisioned hostname (which must be `<label>.trycloudflare.com`, or the
start fails), a session manager, the broker-assertion validator and the handler
that combines them with the recipient policy. The handler goes into
`orchestration.NewOrchestratorWithHTTPRequestAuthorizer` — the place cloudflared
actually consults it — and, for parity, into
`connection.TunnelProperties.QuickTunnelAuthorizer`. The validator owns a JWKS
refresh goroutine and is closed when the round ends, so toggling does not leak.
Importing `quicktunnelauth` pulls in nothing under `cloudflared/cmd/`.

**5. Counts, never addresses.** The status document (`protected`,
`allowedRecipients: {emails, domains}`), the `tunnel.changed` event, the log
lines and the banner say *that* the tunnel is protected and *how many*
addresses and domain rules the list holds. The list itself appears in none of
them: it is personal data, and the places those surfaces end up — a scrolled-back
terminal, a pasted log, a second browser tab — are not where it belongs.
`protected` is reported in every state, including `off`, so the settings card
can say what the switch will do before anyone flips it.

## Consequences

**Good.** A shared board can be restricted to named people or one company's
domain with no Cloudflare account, no DNS and no configuration beyond a list of
addresses. Strangers who find the hostname — it travels through Cloudflare, chat
logs and browser histories — see a Cloudflare login page instead of a live
workspace's interface. A leaked access link stops being a full credential by
itself. Nothing changes for anyone who does not set the list.

**Bad, and accepted.**

- **The gate is exactly as strong as the mailboxes on the list.** A `*@domain`
  rule admits everyone who can receive mail at that domain, and a compromised
  mailbox passes. That is why the token stays.
- **Sessions are process-local.** They live in the connector's memory, so every
  restart, every toggle and every new hostname asks visitors for a fresh PIN.
  For a ten-minute share this is the right lifetime; it is noticeably friction
  for anything longer, which is also the right signal.
- **More of the security model now sits in a dependency we do not own.** The
  authorizer, its cookie and its assertion checks are cloudflared's, pinned to a
  pseudo-version with no compatibility promise. An upgrade that changes the
  `quicktunnelauth` constructors or the orchestrator signature breaks the build
  loudly; one that changes behaviour does not, so the live end-to-end test grows
  a protected variant (an unauthenticated `GET` must be a `302` to a host other
  than the tunnel's), run deliberately like the existing one.
- **The visitor flow depends on Cloudflare Access being up** in addition to the
  edge. If the login broker is down, a protected tunnel is unreachable rather
  than open, which is the failure mode we want.

## Alternatives considered

- **OTP instead of the token for a protected tunnel.** Friendlier: one login,
  no link to pass around. Rejected because the companion would then trust an
  identity assertion it never sees — the session is decided inside cloudflared
  and stripped before the request arrives — and because it would make the
  security of the write API depend on a feature a user can switch off with one
  configuration line. Two independent gates are the point.
- **Protect every tunnel by default with the operator's own address.** The
  product does not know the operator's address, and guessing one from git
  configuration would put personal data into a third-party access policy
  without asking. Opt-in keeps the decision with the person who can make it.
- **Edit the allow-list from the web UI.** Convenient, and a way for anyone
  holding the token to widen who can reach the server while it is published.
  The list is decided at process start, like the token itself.
- **Implement the authorizer ourselves in `internal/server`.** The companion
  would see the session and could audit it. It would also mean reimplementing
  Cloudflare's broker protocol, its assertion format and its key rotation —
  owning security-relevant code that upstream already maintains — and the
  authorization would run after the request crossed into our process rather
  than before.
- **A named tunnel with a Cloudflare Access application.** Stronger policies,
  stable hostnames and audit logs. It needs an account, DNS and policy
  configuration — the different product ADR-027 already set aside.
