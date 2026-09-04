// Package vault implements the CoreApi contract once, for every host.
//
// A Vault owns a file system, the projects discovered in it, the index built
// over them and one core.FileStore per project. It answers the methods declared
// by web/src/core-bridge/api.ts — vault.load, item.list, item.create, kb.write,
// search and the rest — either as a typed result (Dispatch, what the companion
// server maps onto REST) or as the JSON envelope the browser worker expects
// (Call).
//
// The same implementation serves both operating modes:
//
//   - Browser-only. NewInMemory mounts an empty core.MemFS; the host pushes the
//     vault in with "vault.load" and mirrors later changes with "vault.apply".
//     Every mutating call reports a WriteSet the host persists through the File
//     System Access API.
//   - Companion process. Open mounts a directory through internal/core/osfs,
//     discovers the projects and indexes the files directly. Writes are already
//     persisted when the call returns; the WriteSet is still reported, because
//     it is what tells a watcher and the connected UIs which files changed.
//
// The package is deliberately free of os, path/filepath and syscall/js so that
// it compiles for GOOS=js GOARCH=wasm: the file system is always injected by
// the host. Everything platform-specific lives in wasm/ and in the packages
// that build an FS for it.
package vault
