// Package watcher turns file-system events under the documentation folders of
// the registered repositories into batched, debounced change notifications that
// drive incremental re-indexing and the WebSocket event stream.
//
// Responsibilities:
//
//   - wrap fsnotify, which is not recursive on Linux and Windows, by walking the
//     tree and registering every directory below the docs folder, adding and
//     removing watches as directories appear and disappear;
//   - skip .git, node_modules and the configured ignore globs at walk time, to
//     keep the descriptor count in the hundreds;
//   - coalesce the multi-event save patterns of editors within a debounce window
//     (default 250 ms) and reconcile atomic saves, which arrive as a rename;
//   - degrade to a periodic mtime scan when the platform runs out of watches,
//     reporting the degradation instead of failing.
//
// This package is native-only: it is never compiled into the WebAssembly build,
// where the browser reports changes through the File System Access API instead.
// It must not parse item files — that belongs to internal/core.
//
// Implementation lands in Phase 2 with the companion CLI (docs/07 section 6.3).
package watcher
