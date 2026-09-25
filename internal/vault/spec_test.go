package vault

import (
	"strings"
	"testing"
)

// specWriteResult is the shape of an item write that may have raised the schema.
type specWriteResult struct {
	Item struct {
		ID   string `json:"id"`
		Path string `json:"path"`
	} `json:"item"`
	Writes         WriteSet `json:"writes"`
	SchemaUpgraded int      `json:"schemaUpgraded"`
}

func TestVaultSpecCreateUpgradesSchema(t *testing.T) {
	v, _ := loadedVault(t)

	raw := call(t, v, "item.create", map[string]any{
		"project": "DEMO", "type": "spec", "title": "Checkout addresses",
		"body": "## Requirements\n\n### DEMO-SP-0001.R1 — Trim input\n\nThe checkout SHALL trim pasted addresses.\n",
	})
	got := decode[specWriteResult](t, raw)
	if got.Item.ID != "DEMO-SP-0001" || got.Item.Path != "docs/.pmngr/specs/DEMO-SP-0001-checkout-addresses.md" {
		t.Errorf("created %s at %s", got.Item.ID, got.Item.Path)
	}
	if got.SchemaUpgraded != 2 {
		t.Errorf("schemaUpgraded = %d, want 2", got.SchemaUpgraded)
	}
	var project string
	for _, f := range got.Writes.Written {
		if strings.HasSuffix(f.Path, "/project.yaml") {
			project = f.Text
		}
	}
	if !strings.Contains(project, "\nschema: 2\n") {
		t.Errorf("project.yaml is not in the write set at schema 2:\n%s", project)
	}

	// The second spec construct finds the project upgraded: nothing to report.
	raw = call(t, v, "item.create", map[string]any{"project": "DEMO", "type": "spec", "title": "Payments"})
	if again := decode[specWriteResult](t, raw); again.SchemaUpgraded != 0 {
		t.Errorf("second write reported schemaUpgraded = %d", again.SchemaUpgraded)
	}
}

func TestVaultPlainWriteDoesNotUpgrade(t *testing.T) {
	v, _ := loadedVault(t)
	raw := call(t, v, "item.create", map[string]any{"project": "DEMO", "type": "task", "title": "Plain task"})
	got := decode[specWriteResult](t, raw)
	if got.SchemaUpgraded != 0 {
		t.Errorf("schemaUpgraded = %d on a write without a spec construct", got.SchemaUpgraded)
	}
	for _, f := range got.Writes.Written {
		if strings.HasSuffix(f.Path, "/project.yaml") && strings.Contains(f.Text, "schema: 2") {
			t.Errorf("project.yaml upgraded by a plain write")
		}
	}
}

func TestVaultWriteGateOnNewerSchema(t *testing.T) {
	v := NewInMemory()
	files := fixtureFiles(t)
	for _, f := range files {
		if strings.HasSuffix(f["path"], ".pmngr/project.yaml") {
			f["text"] = strings.Replace(f["text"], "\nschema: 1\n", "\nschema: 3\n", 1)
		}
	}
	call(t, v, "vault.load", map[string]any{"files": files})

	env := rawCall(t, v, "item.create", map[string]any{"project": "DEMO", "type": "task", "title": "Refused"})
	if env.OK || env.Error.Code != "read_only" {
		t.Fatalf("item.create on schema 3 = ok %v, code %q, want read_only", env.OK, env.Error.Code)
	}
	// Reads keep working: the project is open read-only, not closed.
	call(t, v, "item.list", map[string]any{})
}
