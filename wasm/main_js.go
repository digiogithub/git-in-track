//go:build js && wasm

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"syscall/js"

	"github.com/digiogithub/git-in-track/internal/core"
)

// version, commit and date are set with -ldflags by the release build, exactly
// as they are for the CLI binary.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

// namespace is the global object the Web Worker talks to.
const namespace = "gintrackCore"

func main() {
	ns := js.Global().Get("Object").New()
	ns.Set("version", js.FuncOf(jsVersion))
	ns.Set("parseItem", js.FuncOf(jsParseItem))
	js.Global().Set(namespace, ns)

	// Keep the Go runtime alive for the lifetime of the worker: every exported
	// function is called from JavaScript long after main would otherwise return.
	select {}
}

// jsVersion reports the build and the data-model schema this module implements.
//
//	gintrackCore.version() -> {"version": "...", "commit": "...", "date": "...", "schema": 1}
func jsVersion(js.Value, []js.Value) any {
	return result(map[string]any{
		"version": version,
		"commit":  commit,
		"date":    date,
		"schema":  core.SupportedSchema,
	})
}

// jsParseItem parses one item file and returns it as JSON.
//
//	gintrackCore.parseItem(path, text) -> {"ok": true, "data": {...}}
//	                                   -> {"ok": false, "error": {"code": "...", "message": "..."}}
func jsParseItem(_ js.Value, args []js.Value) any {
	if len(args) < 2 {
		return failure("bad_request", "parseItem(path, text) needs two arguments")
	}
	path := args[0].String()
	item, err := core.ParseItem(path, []byte(args[1].String()))
	if err != nil {
		return failure(errorCode(err), err.Error())
	}
	return result(item)
}

// errorCode maps a core error onto the stable code catalog the UI switches on.
func errorCode(err error) string {
	var pe *core.ParseError
	if errors.As(err, &pe) {
		return string(pe.Code)
	}
	return "validation_failed"
}

// result marshals a successful payload into the envelope the bridge expects.
func result(payload any) any {
	data, err := json.Marshal(payload)
	if err != nil {
		return failure("internal", fmt.Sprintf("encode result: %v", err))
	}
	return string(mustEnvelope(`{"ok":true,"data":`, data))
}

// failure builds the error envelope, mirroring the RFC 7807 code catalog.
func failure(code, message string) any {
	data, err := json.Marshal(map[string]string{"code": code, "message": message})
	if err != nil {
		return `{"ok":false,"error":{"code":"internal","message":"encode error"}}`
	}
	return string(mustEnvelope(`{"ok":false,"error":`, data))
}

// mustEnvelope wraps an already encoded payload in the envelope.
func mustEnvelope(prefix string, payload []byte) []byte {
	out := make([]byte, 0, len(prefix)+len(payload)+1)
	out = append(out, prefix...)
	out = append(out, payload...)
	return append(out, '}')
}
