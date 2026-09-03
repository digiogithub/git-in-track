# ADR-004 — Browser-only mode via the File System Access API

- **Status:** Accepted
- **Date:** 2026-09-03
- **Phase:** 1 (Browser-only MVP)
- **Related:** [ADR-003](ADR-003-shared-go-core-wasm.md), [ADR-005](ADR-005-companion-cli-go-embed.md), [ADR-006](ADR-006-isomorphic-git-vs-go-git.md)

## Context

The single largest barrier to adoption for a developer tool is installation. A PM
evaluating git-in-track will not download a binary, and a developer evaluating it on
a work laptop may not be allowed to. We want the first useful experience to be:
open a URL, pick a folder, see your backlog.

Browsers now offer real local filesystem access. The File System Access API
(`showDirectoryPicker`) grants a page a user-selected directory handle with
read/write permission, persistable in IndexedDB across sessions. It is shipped in
Chromium-based browsers (Chrome, Edge, Opera, Brave, Arc). Firefox and Safari have
not shipped it; Safari supports it only for the Origin Private File System, which is
sandboxed and therefore useless for opening a user's git checkout.

The alternatives for local access in a browser are: `<input webkitdirectory>` (read
a directory tree once, no write, no persistence) and drag-and-drop (same
limitations).

## Decision

**Browser-only mode uses the File System Access API as its filesystem, and is a
complete, first-class mode — not a demo.**

- `showDirectoryPicker({ mode: 'readwrite' })` obtains a handle to the project or
  team folder. Handles are persisted in IndexedDB so returning is one click plus a
  permission re-grant where the browser requires one.
- The WASM core's `host.FS` implementation is backed by these handles, running in a
  Web Worker.
- Full CRUD on the backlog, full knowledge-base rendering, and (Phase 4) git
  operations through isomorphic-git over the same handles.
- **Documented graceful degradation.** On browsers without the API, the app offers a
  read-only mode via `<input webkitdirectory>`: the user can browse the knowledge
  base and the backlog for the session, and the UI states explicitly, once and
  clearly, that editing requires a Chromium-based browser or the `gintrack`
  companion. We do not hide the limitation and we do not fake writing.
- The capability is detected at runtime, never inferred from the user agent string.

## Consequences

**Positive**

- Zero installation for the primary path. The evaluation loop is a URL and a folder
  picker.
- Real local files: the user's own git checkout, edited in place, visible in
  `git status` in their terminal.
- The permission model is the browser's, and it is explicit and revocable — a
  stronger privacy story than a native app that can read anything.
- The app can be hosted as a static site or served by the companion; identical code.

**Negative**

- **Chromium-only for the full experience.** A meaningful share of developers use
  Firefox or Safari and will get a read-only fallback until they install the
  companion. This is the single biggest known limitation of Phase 1 and must be
  stated in the README, not buried.
- **No filesystem events.** The browser cannot watch a directory. External changes
  (an editor, an agent, a `git pull`) are detected by polling open documents and by
  a full rescan on focus or explicit refresh. This asymmetry is the clearest reason
  to install the companion.
- **Permission friction.** Some browsers re-prompt for a persisted handle after a
  restart; the UX must make re-granting a single obvious click rather than a
  mysterious failure.
- **Performance ceiling.** Per-file handle operations are slower than native
  `os` calls, and the WASM budget for a 10,000-item cold scan is 8 s versus 2 s
  natively. The IndexedDB cache does most of the work of hiding this.
- **No `.git` shortcuts.** Everything git-related must go through isomorphic-git
  over the same handle-based filesystem; no system git, no credential helper, no
  SSH agent ([ADR-006](ADR-006-isomorphic-git-vs-go-git.md)).
- **Storage limits.** IndexedDB quota is finite and eviction is possible; the cache
  must be treated as disposable and its absence must only cost time.

## Alternatives considered

- **Origin Private File System (OPFS) with import/export.** Works in every modern
  browser and is fast, but the vault would live in a browser-private sandbox, not in
  the user's git checkout. Sync would require explicit import/export of the whole
  tree. That breaks the premise that the repository is the state. Rejected as the
  primary mechanism; still under consideration as an optional cache location.
- **`<input webkitdirectory>` as the primary path.** Read-only and non-persistent;
  cannot be the main mode. Retained as the fallback.
- **Requiring the companion CLI for everything.** Simplifies the architecture
  substantially and removes the Chromium constraint, at the cost of the zero-install
  adoption path and the browser-only mode that makes the project approachable.
  Rejected.
- **A browser extension for filesystem access.** More capability, but a store review
  process, per-browser implementations, and an install step that is not smaller than
  installing the CLI. Rejected.
- **An Electron or Tauri desktop app instead of a browser app.** Removes API
  limitations but adds a large distribution artifact and abandons "open a URL".
  Possible future packaging of the same web app; not the Phase 1 path.
