# ADR-003 — A shared Go core compiled natively and to WebAssembly

- **Status:** Accepted
- **Date:** 2026-09-03
- **Phase:** 0 (Foundations)
- **Related:** [ADR-004](ADR-004-browser-only-file-system-access.md), [ADR-005](ADR-005-companion-cli-go-embed.md), [ADR-009](ADR-009-react-vite-typescript.md)

## Context

git-in-track runs the same logic in two very different places:

- In the **browser**, with no installation, reading folders through the File System
  Access API (Phase 1).
- In the **CLI companion**, natively, with fsnotify, native git and a local HTTP API
  (Phase 2). The same core also backs the **MCP server** (Phase 5).

That logic is substantial and semantically load-bearing: front-matter parsing and
stable re-serialisation, the typed model, validation and workflow rules, ID parsing
and allocation, indexing, the query engine, the search index, and the link graph.

If it is implemented twice — Go for the CLI, TypeScript for the browser — the two
implementations *will* diverge. Not spectacularly, but in the way that matters: a
different YAML quoting choice, a different empty-list representation, a different
`rev` normalisation, a different ordering of allocated IDs. The user sees it as a
spurious diff on every save depending on which mode they used, or as an item that is
valid in one mode and invalid in the other. Every divergence is a bug report that
costs two fixes.

Go compiles to `js/wasm` out of the box. The core has no inherent need for `os`,
`net` or `syscall` if I/O is abstracted behind an interface.

## Decision

**There is exactly one implementation of the model, in Go, in `internal/core`. It is
compiled twice: natively for the CLI, and with `GOOS=js GOARCH=wasm` for the
browser.**

- `internal/core` must not import `os`, `net`, `net/http`, `os/exec`, or anything
  that breaks the `js/wasm` build. This is enforced by a lint rule and by a
  `GOOS=js GOARCH=wasm go build ./...` step in CI.
- All I/O goes through the `core/host` interfaces (`FS`, `Cache`, `Clock`,
  `Progress`). Native builds implement them with `os`; the WASM build implements
  them by calling back into JavaScript (File System Access handles, IndexedDB).
- `wasm/main_js.go` exposes a single entry point to JS and is loaded inside a **Web
  Worker**, so core work never blocks the main thread.
- The bridge protocol (JSON envelope with request ids, streaming progress, typed
  errors) is the same shape used by the REST API and the MCP tools, so an operation
  is specified once.
- A **parity test suite** runs the same fixture corpus through both builds and fails
  CI on any difference in index, query or search output.

The TypeScript layer renders, edits and orchestrates. It contains no model
semantics: no front-matter parser, no validator, no ID allocator.

## Consequences

**Positive**

- One behaviour, provable by the parity suite. A bug is fixed once.
- Go's testing, benchmarking and fuzzing apply to the parser and indexer — the
  components where a subtle bug is most expensive.
- Performance is good in both targets: native meets the sub-2 s / 10k-item budget,
  and WASM in a worker meets the sub-8 s budget.
- The MCP server and the REST API get the model for free; agents and humans cannot
  observe different semantics.

**Negative**

- **Bundle size.** A Go WASM binary is large; we budget < 3 MB Brotli-compressed and
  must actively defend it (no reflection-heavy dependencies, no bleve, careful with
  `encoding/json` alternatives). TinyGo is not viable given our dependencies.
- **Bridge friction.** Every core call crosses a serialisation boundary. Chatty
  designs are pathological; the API must be coarse-grained, and large payloads must
  travel as transferable `ArrayBuffer`s.
- **Debugging is harder.** Stack traces from WASM are poor; we mitigate with
  structured errors carrying explicit context, and by making the native build the
  primary debugging target.
- **Memory.** The Go runtime plus GC in WASM has a real floor and the heap does not
  shrink readily; the index deliberately does not retain bodies.
- **Build complexity.** Two build targets, `wasm_exec.js` version coupling to the Go
  toolchain, and a worker that must load a fingerprinted artifact. `make wasm` and
  CI checks absorb this.
- **Team skill.** Contributors touching the core need Go; front-end-only
  contributors are limited to `web/`. Accepted: the boundary is clean and
  documented.

## Alternatives considered

- **Two implementations (Go + TypeScript).** Best developer ergonomics per side,
  guaranteed drift, double the tests. This is precisely the failure mode this ADR
  exists to prevent. Rejected.
- **Rust core compiled to WASM plus a native library.** Smaller and faster WASM
  output and excellent tooling, but the CLI, server, watcher, git layer and MCP
  server would still be Go (or would all have to become Rust). Two languages in one
  repository for one component is a worse trade than a larger WASM binary. Rejected.
- **TypeScript core, run in Node for the CLI.** Would unify on one language, but
  gives up the single static binary, `go:embed`, goroutine-based watching and
  indexing performance, and forces a Node runtime on every user. Rejected.
- **TinyGo for the WASM build.** Much smaller output, but incomplete `reflect` and
  standard-library support breaks `yaml.v3` and other dependencies, and — critically
  — it would mean the browser runs *different code*, reintroducing drift by the back
  door. Rejected.
- **Server-only core, with the browser always talking to the companion.** Simplest
  architecture, but it deletes the zero-install browser-only mode that is the
  project's main adoption path. Rejected.
