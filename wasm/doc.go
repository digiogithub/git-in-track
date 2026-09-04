// Package main builds the WebAssembly module that carries the shared Go core
// into the browser for browser-only mode.
//
// The module is compiled with `make wasm` (GOOS=js GOARCH=wasm) into
// web/public/core.wasm and is loaded by a Web Worker together with the
// wasm_exec.js of the same Go release. It registers a namespace on globalThis
// and marshals every argument and result as JSON, so the TypeScript types in
// web/src/core mirror the same Go structs the REST API serves.
//
// This package is glue and nothing else: the CoreApi contract it exposes is
// implemented once, natively, in internal/vault, which the companion server
// serves over HTTP as well. Nothing here may hold domain logic.
//
// The build-tagged entry point lives in main_js.go; this file carries the
// package documentation and keeps the package non-empty for the native
// toolchain, so `go vet ./...` and `go build ./...` do not fail on a package
// whose only file is excluded by a build tag.
package main
