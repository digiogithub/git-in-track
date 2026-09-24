package vault

import (
	"strings"
	"testing"
)

// TestItemCreateKeepsProjectYAMLByteStable reproduces GIT-US-0153: creating an
// item rewrote project.yaml through a full YAML re-encode, which re-emitted a
// flow-style label whose description holds commas as
// `description: Shared Go core (model, parser: ”, index): ”`. The counter
// write must now touch the counter and nothing else.
func TestItemCreateKeepsProjectYAMLByteStable(t *testing.T) {
	files := fixtureFiles(t)
	const projectPath = "docs/.pmngr/project.yaml"
	var original string
	for _, f := range files {
		if f["path"] != projectPath {
			continue
		}
		text := strings.Replace(f["text"],
			"  - { name: payments, color: \"#0891b2\" }\n",
			"  - { name: payments, color: \"#0891b2\" }\n"+
				"  - { name: core,     color: \"#4f46e5\", description: Shared Go core (model, parser, index) }\n"+
				"  - { name: newcomer, color: \"#16a34a\", description: \"Small, well-scoped, good for newcomers\" }\n", 1)
		if text == f["text"] {
			t.Fatal("the fixture's label block moved; update this test")
		}
		f["text"] = text
		original = text
	}
	if original == "" {
		t.Fatalf("%s is missing from the fixture", projectPath)
	}

	v := NewInMemory()
	call(t, v, "vault.load", map[string]any{"files": files})
	raw := call(t, v, "item.create", map[string]any{
		"project": "DEMO", "type": "task", "title": "Keep project.yaml intact",
		"parent": "DEMO-US-0001", "author": "claude",
	})
	created := decode[struct {
		Writes WriteSet `json:"writes"`
	}](t, raw)

	var got string
	for _, f := range created.Writes.Written {
		if f.Path == projectPath {
			got = f.Text
		}
	}
	if got == "" {
		t.Fatal("write_counters is on, so project.yaml must be in the WriteSet")
	}
	want := strings.Replace(original, "    task: 1\n", "    task: 2\n", 1)
	if got != want {
		t.Errorf("project.yaml changed beyond the task counter:\n--- got\n%s\n--- want\n%s", got, want)
	}
	for _, bad := range []string{"parser: ''", "well-scoped: ''"} {
		if strings.Contains(got, bad) {
			t.Errorf("a label description was split into extra keys (%q)", bad)
		}
	}
}
