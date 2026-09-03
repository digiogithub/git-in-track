# ADR-006 — isomorphic-git in the browser, go-git (or system git) in the CLI

- **Status:** Accepted
- **Date:** 2026-09-03
- **Phase:** 4 (Git sync)
- **Related:** [ADR-002](ADR-002-git-as-only-sync.md), [ADR-003](ADR-003-shared-go-core-wasm.md), [ADR-004](ADR-004-browser-only-file-system-access.md)

## Context

Git is the only sync mechanism ([ADR-002](ADR-002-git-as-only-sync.md)), so both
operating modes need to perform status, add, commit, fetch, merge/rebase, push and
conflict enumeration.

The two environments have incompatible constraints:

- **Browser.** No process execution, no raw TCP, no SSH. HTTP(S) only, subject to
  CORS: most git hosts do not send `Access-Control-Allow-Origin` on their smart HTTP
  endpoints, so a browser cannot talk to them directly. `isomorphic-git` is the
  mature JavaScript implementation and works over an injectable filesystem, which
  maps onto our File System Access handles.
- **CLI.** Can do anything. `go-git` is a pure-Go implementation with no external
  dependency; the system `git` binary is the reference implementation with full
  support for credential helpers, SSH agents, signing, hooks, LFS, partial clone and
  every host quirk.

A single implementation for both is not available: compiling `go-git` to WASM does
not solve the transport problem (no SSH, still CORS-bound), and it would add
significant weight to a WASM budget we are already defending
([ADR-003](ADR-003-shared-go-core-wasm.md)).

## Decision

**Use different git implementations per mode, behind one `GitOps` interface defined
once, with mode-specific capabilities advertised explicitly to the UI.**

- **Browser-only mode:** `isomorphic-git`, running on the main thread or a dedicated
  worker, over a filesystem adapter backed by File System Access handles. HTTPS
  transport only, with token authentication. Where a remote lacks CORS headers, a
  user-configured CORS proxy is required; the UI detects the failure and explains it
  rather than showing a generic network error.
- **Companion mode:** `internal/gitops` with two interchangeable backends:
  - `system-git` (`os/exec`) — **the default when a `git` binary is present**,
    because it inherits the user's existing authentication, signing and LFS setup;
  - `go-git` — the fallback, always available, no external dependency.
  The backend is configurable (`git.backend: auto | system | go-git`).
- The `GitOps` interface is the *only* git abstraction; the UI never calls a git
  implementation directly. It exposes a `Capabilities()` result (SSH support, signing,
  LFS, shallow fetch, submodules), and the UI hides or disables what the current mode
  cannot do instead of failing at the last step.
- Git logic lives **outside** `internal/core` (the core never performs I/O), so this
  split does not compromise the shared-core decision: the *model* is shared even
  though the *transport* is not.

## Consequences

**Positive**

- Each environment uses the best available tool: browser users get real git with no
  installation; CLI users get the full fidelity of the git they already configured.
- Keeping git out of the core preserves the WASM budget and the "core has no I/O"
  rule.
- `system-git` by default means we inherit years of edge-case handling for free, and
  enterprise setups (SSH certificates, credential managers, proxies) work on day one.

**Negative**

- **Two git behaviours to test and support.** Conflict enumeration, rebase semantics
  and error messages differ. The `GitOps` interface must normalise them, and the test
  suite must cover both against the same fixture remotes.
- **CORS is a real, visible limitation.** Many hosts require a proxy for in-browser
  git. This must be documented prominently as a known limitation of browser-only
  mode, with a clear remedy (configure a proxy, or install the companion).
- **No SSH in the browser.** Browser-only users must use HTTPS with a token, which
  means token handling in browser storage and a security note about it.
- **Performance.** In-browser clone and fetch of a large repository is slow and
  memory-hungry. We mitigate by preferring fetch over clone, supporting shallow and
  single-branch fetches, and steering users with large repositories to the companion.
- **`go-git` gaps.** It does not cover every git feature (some rebase/merge cases,
  hooks, LFS). Choosing `system-git` by default keeps `go-git` on the less-travelled
  path, but the gaps must be documented where it is used.
- **Dependency risk.** `isomorphic-git` is a substantial third-party dependency in a
  security-relevant position; it is pinned, audited on upgrade, and confined behind
  the interface.

## Alternatives considered

- **`go-git` compiled to WASM for the browser.** One implementation and a shared code
  path, but it neither solves CORS nor enables SSH, and it inflates the WASM bundle
  well past budget. Rejected.
- **`isomorphic-git` everywhere via Node in the CLI.** Would require a Node runtime,
  contradicting the single static binary. Rejected.
- **System `git` only, in both modes (companion mandatory for sync).** Simplest and
  most correct, but it deletes git support from browser-only mode, which is where
  most first-time users start. Rejected; browser-only sync is a Phase 4 goal.
- **Shipping our own CORS proxy as a hosted service.** Convenient and directly
  contrary to [ADR-002](ADR-002-git-as-only-sync.md): a service we operate, that sees
  user tokens and repository content. Rejected. A self-hostable `gintrack proxy`
  remains an open question.
- **Implementing a minimal git client ourselves.** Weeks of work to reach a worse
  place than either existing library. Rejected.
