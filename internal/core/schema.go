package core

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
)

// The project schema gate and the implicit 1 → 2 upgrade of ADR-037 section 11
// (docs/03 section 21.10, R-EVO-2).

// ErrSchemaUnsupported reports a write refused because project.yaml declares no
// schema, or one newer than SupportedSchema. It always travels together with
// ErrReadOnly, so that every host maps it onto its read-only answer.
var ErrSchemaUnsupported = errors.New("project schema is not writable by this build")

// SchemaGateError is the refusal of a write to a project whose schema this
// build cannot write (R-EVO-2).
type SchemaGateError struct {
	// Schema is the schema project.yaml declares; zero when it declares none.
	Schema int
}

// Error implements the error interface.
func (e *SchemaGateError) Error() string {
	if e.Schema == 0 {
		return fmt.Sprintf("%s declares no schema: the project is open read-only", ProjectFileName)
	}
	return fmt.Sprintf("schema %d is newer than the supported version %d: the project is open read-only",
		e.Schema, SupportedSchema)
}

// Unwrap classifies the refusal as both a schema problem and a read-only file
// system, which is what the vault and the CLI switch on.
func (e *SchemaGateError) Unwrap() []error { return []error{ErrSchemaUnsupported, ErrReadOnly} }

// WriteGate reports whether this build may write to the project: every write is
// refused while project.yaml declares no schema or one newer than
// SupportedSchema (R-EVO-2, R-SCHEMA-2-4). A nil configuration is not gated;
// the caller has nothing to write into anyway.
func (p *ProjectConfig) WriteGate() error {
	if p == nil {
		return nil
	}
	if p.Schema == 0 || p.Schema > SupportedSchema {
		return &SchemaGateError{Schema: p.Schema}
	}
	return nil
}

// schemaLineRE matches the top-level `schema:` line of project.yaml with a plain
// integer value and an optional comment.
var schemaLineRE = regexp.MustCompile(`(?m)^schema:([ \t]*)(\d+)([ \t]*(?:#.*)?)$`)

// UpgradeProjectSchema returns project.yaml with its schema raised to `to`.
// It changes that one line and nothing else whenever the line is written in the
// ordinary `schema: 1` form, so the upgrade is a one-line diff (R-SCHEMA-2-3);
// any other shape falls back to editing the YAML node tree. It returns nil when
// the file already declares `to` or more: there is no downgrade.
func UpgradeProjectSchema(data []byte, to int) ([]byte, error) {
	cfg, err := LoadProjectConfig(data)
	if cfg == nil {
		return nil, fmt.Errorf("upgrade schema: %w", err)
	}
	if cfg.Schema >= to {
		return nil, nil
	}
	if locs := schemaLineRE.FindAllSubmatchIndex(data, -1); len(locs) == 1 {
		loc := locs[0]
		out := make([]byte, 0, len(data)+1)
		out = append(out, data[:loc[4]]...)
		out = append(out, strconv.Itoa(to)...)
		out = append(out, data[loc[5]:]...)
		if check, _ := LoadProjectConfig(out); check != nil && check.Schema == to {
			return out, nil
		}
	}
	out, err := setYAMLPath(data, []string{"schema"}, strconv.Itoa(to))
	if err != nil {
		return nil, fmt.Errorf("upgrade schema: %w", err)
	}
	return out, nil
}
