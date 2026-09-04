// Package core implements the shared, platform-independent model of a git-in-track
// vault: the item model, YAML front-matter parsing and serialization, identifiers,
// the rev content hash and the project configuration.
//
// # Portability contract
//
// This package is compiled twice: natively for the CLI and the companion server, and
// to WebAssembly (GOOS=js GOARCH=wasm) for the browser-only mode. It therefore MUST
// stay free of OS-specific and browser-specific imports:
//
//   - no "os", "os/exec", "syscall" or "syscall/js";
//   - no "net/http", "fsnotify", go-git or any other host-only dependency;
//   - no "path/filepath" — all paths inside a vault are forward-slash, relative
//     paths handled with "path";
//   - no walking of the file system: every byte is read and written through the FS
//     interface supplied by the caller.
//
// Native file access lives in the sibling package internal/core/osfs, browser glue
// lives in wasm/, and everything else that needs the operating system lives in
// internal/server, internal/watcher or internal/gitops.
//
// The on-disk format this package implements is specified in docs/03-data-model.md;
// rule identifiers such as R-FMT-1 or E-ID-GRAMMAR in the comments below refer to
// that document.
package core
