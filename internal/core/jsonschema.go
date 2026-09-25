package core

import (
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

// The JSON Schemas (Draft 2020-12) of docs/03 section 18: one per item type,
// one for comments, one for project.yaml and the shared $defs they reference.
// They are embedded, so the CLI and the WebAssembly build ship the same bytes,
// and jsonschema_test.go fails whenever they drift from the Go model.
//
//go:embed schema/*.json
var jsonSchemaFiles embed.FS

// JSONSchemaBaseURI is the base every schema's $id is resolved against.
const JSONSchemaBaseURI = "https://git-in-track.dev/schema/"

// JSONSchemaDraft is the $schema every shipped schema declares.
const JSONSchemaDraft = "https://json-schema.org/draft/2020-12/schema"

// JSONSchemaNames lists the file names of the shipped schemas, sorted, e.g.
// "common.defs.json" and "story.schema.json".
func JSONSchemaNames() []string {
	entries, err := fs.ReadDir(jsonSchemaFiles, "schema")
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names
}

// JSONSchema returns the bytes of one shipped schema by file name.
func JSONSchema(name string) ([]byte, error) {
	if strings.ContainsAny(name, `/\`) {
		return nil, fmt.Errorf("json schema %q: %w", name, fs.ErrNotExist)
	}
	data, err := jsonSchemaFiles.ReadFile("schema/" + name)
	if err != nil {
		return nil, fmt.Errorf("json schema %q: %w", name, err)
	}
	return data, nil
}

// JSONSchemaFor returns the file name of the schema that describes the front
// matter of an item type, e.g. "spec.schema.json". It reports false for a type
// this build does not know.
func JSONSchemaFor(t ItemType) (string, bool) {
	if !t.Valid() {
		return "", false
	}
	return string(t) + ".schema.json", true
}
