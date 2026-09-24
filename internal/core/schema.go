package core

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
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

// ErrSchemaMigration classifies every refusal of an explicit schema migration
// (`gintrack migrate --to N`, R-EVO-4); ErrSchemaDowngrade narrows it to a
// target below the schema the project already declares.
var (
	ErrSchemaMigration = errors.New("schema migration refused")
	ErrSchemaDowngrade = errors.New("schema downgrade refused")
)

// SchemaMigrationError is a refused migration of one project.yaml. From is
// the declared schema, zero when the file declares none; To is the target.
type SchemaMigrationError struct {
	From, To  int
	Downgrade bool
	Reason    string
}

// Error implements the error interface.
func (e *SchemaMigrationError) Error() string { return e.Reason }

// Unwrap classifies the refusal, so that callers can tell a downgrade from
// every other refusal with errors.Is.
func (e *SchemaMigrationError) Unwrap() []error {
	if e.Downgrade {
		return []error{ErrSchemaMigration, ErrSchemaDowngrade}
	}
	return []error{ErrSchemaMigration}
}

// SchemaMigration is the plan of an explicit schema migration of one
// project.yaml (R-EVO-4). It never touches any other file: the only migration
// the data model defines, 1 → 2, needs no content change (R-EVO-6).
type SchemaMigration struct {
	// From is the schema the file declares and To the target.
	From int `json:"from"`
	To   int `json:"to"`
	// Data is the rewritten file; nil when the file already declares To, which
	// is what makes the migration idempotent.
	Data []byte `json:"-"`
	// Removed and Added are the lines that differ between the two versions, in
	// file order: the printed diff summary.
	Removed []string `json:"removed,omitempty"`
	Added   []string `json:"added,omitempty"`
}

// Changed reports whether the migration writes anything.
func (m *SchemaMigration) Changed() bool { return m != nil && m.Data != nil }

// PlanSchemaMigration plans raising project.yaml to schema `to`. The rewrite is
// UpgradeProjectSchema, the same one-line edit the implicit upgrade on first
// use and `doctor --fix` make (R-SCHEMA-2-3). It refuses a target this build
// cannot write, a file that declares no schema or a newer one, and any
// downgrade: there is no downgrade path (R-SCHEMA-2-3).
func PlanSchemaMigration(data []byte, to int) (*SchemaMigration, error) {
	if to < InitialSchema || to > SupportedSchema {
		return nil, &SchemaMigrationError{To: to, Reason: fmt.Sprintf(
			"schema %d is not a target this build can write: it supports schema %d to %d",
			to, InitialSchema, SupportedSchema)}
	}
	cfg, err := LoadProjectConfig(data)
	if cfg == nil {
		return nil, fmt.Errorf("migrate schema: %w", err)
	}
	from := cfg.Schema
	switch {
	case from == 0:
		return nil, &SchemaMigrationError{To: to, Reason: fmt.Sprintf(
			"%s declares no schema: add `schema: %d` by hand, then migrate", ProjectFileName, InitialSchema)}
	case from > SupportedSchema:
		return nil, &SchemaMigrationError{From: from, To: to, Downgrade: true, Reason: fmt.Sprintf(
			"%s declares schema %d, newer than the supported version %d: upgrade gintrack instead; there is no downgrade",
			ProjectFileName, from, SupportedSchema)}
	case from > to:
		return nil, &SchemaMigrationError{From: from, To: to, Downgrade: true, Reason: fmt.Sprintf(
			"refusing to downgrade %s from schema %d to %d: there is no downgrade (docs/03 R-SCHEMA-2-3); "+
				"revert the commit that raised it if the upgrade was a mistake", ProjectFileName, from, to)}
	case from == to:
		return &SchemaMigration{From: from, To: to}, nil
	}
	out, err := UpgradeProjectSchema(data, to)
	if err != nil {
		return nil, fmt.Errorf("migrate schema: %w", err)
	}
	m := &SchemaMigration{From: from, To: to, Data: out}
	m.Removed, m.Added = changedLines(string(data), string(out))
	return m, nil
}

// changedLines returns the lines of a that b replaces and the lines b puts in
// their place, after trimming the lines both share at the start and the end.
// For the one-line schema edit it is exactly that one line on each side.
func changedLines(a, b string) (removed, added []string) {
	al, bl := strings.Split(a, "\n"), strings.Split(b, "\n")
	start := 0
	for start < len(al) && start < len(bl) && al[start] == bl[start] {
		start++
	}
	ea, eb := len(al), len(bl)
	for ea > start && eb > start && al[ea-1] == bl[eb-1] {
		ea--
		eb--
	}
	return al[start:ea], bl[start:eb]
}
