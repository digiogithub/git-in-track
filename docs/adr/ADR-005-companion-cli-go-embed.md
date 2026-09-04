# ADR-005 — Companion CLI serving the embedded web app with `go:embed`

- **Status:** Accepted
- **Date:** 2026-09-03
- **Phase:** 2 (Companion CLI)
- **Related:** [ADR-003](ADR-003-shared-go-core-wasm.md), [ADR-004](ADR-004-browser-only-file-system-access.md), [ADR-011](ADR-011-goreleaser-unsigned-artifacts.md)

## Context

Browser-only mode ([ADR-004](ADR-004-browser-only-file-system-access.md)) has three
structural limits: no filesystem events, slower indexing, and no access to system
git (credential helpers, SSH agents, signed commits, LFS). Users who adopt
git-in-track seriously will hit all three.

The remedy is a small local process. The design question is what that process is and
how the UI reaches it. Options range from a native desktop shell (Electron/Tauri) to
a headless daemon plus a separately hosted web app.

Constraints we care about: one artifact to download, no runtime prerequisites, works
identically on Linux, macOS and Windows, and — critically — **the same UI code** as
browser-only mode, so we do not maintain two front ends.

## Decision

**Ship a single static Go binary, `gintrack`, that embeds the built web app with
`go:embed` and serves it from a loopback HTTP server.**

- `gintrack serve` binds `127.0.0.1:7317` by default, serves `web/dist` (embedded at
  build time via `//go:embed all:web/dist`), and exposes a chi REST API plus a
  WebSocket event channel under `/api/v1`.
- The server owns the native capabilities: `internal/watcher` (fsnotify) for live
  change events, `internal/core` compiled natively for fast indexing with an on-disk
  cache in `~/.cache/gintrack`, and `internal/gitops` for real git.
- `gintrack mcp` runs the MCP server over stdio; the same tools are also mounted as a
  streamable HTTP endpoint on the local server
  ([ADR-010](ADR-010-mcp-agent-surface.md)).
- The web app **auto-detects** the companion by probing `/api/v1/ping`, and upgrades
  its data source at runtime. This works whether the UI was loaded from the
  companion itself or from a static deployment.
- The UI is written against one `DataSource` interface with two implementations
  (WASM bridge, REST/WS client). No feature code branches on mode.
- Security: loopback binding only, a random per-session bearer token written to a
  `0600` file, `Origin` and `Host` allowlists, and path confinement to registered
  roots (see the architecture document).

## Consequences

**Positive**

- One download, no runtime, no installer, no admin rights. `gintrack serve` and a
  browser tab.
- Live updates: an edit from Vim, an agent, or `git checkout` appears in the UI
  within a couple of hundred milliseconds.
- Real git: whatever authentication the user's `git` already does keeps working,
  including hardware-backed SSH keys and commit signing.
- The binary is also the MCP server and the batch indexer — one artifact, several
  entry points.
- Embedding means the served UI and the API are always the same version; no
  cross-version mismatch to debug.

**Negative**

- **Binary size.** Embedding `web/dist` plus `core.wasm` adds several megabytes. We
  accept it; a ~30 MB single binary is unremarkable in Go tooling.
- **Rebuild coupling.** Changing the front end requires rebuilding the Go binary to
  ship it. Mitigated in development by a `--dev-web-dir` flag that serves from disk
  with the Vite dev server proxying the API.
- **Version skew when the UI is hosted elsewhere.** A statically deployed newer UI
  may talk to an older companion. Mitigated by an `apiVersion` in `/ping` and an
  explicit compatibility check that refuses to upgrade and explains why.
- **Port conflicts.** 7317 may be taken. The server must fail with a clear message
  and support `--port`, and the UI's probe must handle a stranger answering on that
  port (the token and an `apiVersion` field make that safe).
- **A local HTTP server is an attack surface.** Small, but real: hence the token,
  origin checks, host checks and DNS-rebinding protection, all of which must be
  implemented and tested before this ships.
- **Two data paths to test.** Every feature must be exercised in both modes; the
  Playwright suite runs against both.

## Alternatives considered

- **Electron or Tauri desktop app.** Best OS integration and no browser API limits,
  but a much larger artifact (Electron), a system webview dependency and platform
  build toolchains (Tauri), plus code signing expectations. It also abandons the
  "open a URL" story that browser-only mode gives us. Reconsider post-1.0 as an
  additional packaging of the same web app.
- **Headless daemon plus a separately hosted web app.** Effectively what we support
  as a secondary configuration, but as the *only* option it means two things to
  install or trust and a mandatory cross-origin setup. Rejected as the default.
- **Serving `web/dist` from disk next to the binary.** Simpler builds, but turns one
  artifact into a directory the user can break, and makes version skew a local
  problem. Rejected.
- **A native GUI (Fyne, Wails).** Would require a second UI implementation, directly
  contradicting the one-front-end constraint. Rejected.
- **Node-based local server.** Requires Node on every machine and gives up the single
  static binary. Rejected.
