//go:build !js || !wasm

package main

import (
	"fmt"
	"os"
)

// main exists so that the package builds and vets on the host toolchain, where
// the real entry point in main_js.go is excluded by its build tag. Running the
// resulting binary is a mistake, so it says how to build the module properly.
func main() {
	fmt.Fprintln(os.Stderr, "gintrack: this package is the WebAssembly core; build it with")
	fmt.Fprintln(os.Stderr, "  GOOS=js GOARCH=wasm go build -o web/public/core.wasm ./wasm")
	fmt.Fprintln(os.Stderr, "or simply: make wasm")
	os.Exit(2)
}
