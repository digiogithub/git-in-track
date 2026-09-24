package core

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestSetYAMLPathIsByteStable(t *testing.T) {
	t.Parallel()

	const labels = "labels:\n" +
		"  - { name: core,     color: \"#4f46e5\", description: Shared Go core (model, parser, index) }\n" +
		"  - { name: newcomer, color: \"#16a34a\", description: \"Small, well-scoped, good for newcomers\" }\n"
	cases := []struct {
		name  string
		in    string
		keys  []string
		value string
		want  string
	}{
		{
			name:  "replaces an existing counter and nothing else",
			in:    "key: ACME\n\nid_allocation:\n  counters:               # hints only\n    task: 9\n    story: 3\n\n" + labels,
			keys:  []string{"id_allocation", "counters", "task"},
			value: "10",
			want:  "key: ACME\n\nid_allocation:\n  counters:               # hints only\n    task: 10\n    story: 3\n\n" + labels,
		},
		{
			name:  "adds a missing counter after the last one",
			in:    "id_allocation:\n  counters:\n    task: 9\n\n" + labels,
			keys:  []string{"id_allocation", "counters", "epic"},
			value: "2",
			want:  "id_allocation:\n  counters:\n    task: 9\n    epic: 2\n\n" + labels,
		},
		{
			name:  "creates missing intermediate mappings",
			in:    "id_allocation:\n  strategy: scan\n  counters:\n    task: 9\n" + labels,
			keys:  []string{"id_allocation", "redirects", "ACME-US-0007"},
			value: "ACME-US-0012",
			want:  "id_allocation:\n  strategy: scan\n  counters:\n    task: 9\n  redirects:\n    ACME-US-0007: ACME-US-0012\n" + labels,
		},
		{
			name:  "creates a missing top-level section at the end",
			in:    "key: ACME\n" + labels,
			keys:  []string{"id_allocation", "counters", "task"},
			value: "1",
			want:  "key: ACME\n" + labels + "id_allocation:\n  counters:\n    task: 1\n",
		},
		{
			name:  "keeps CRLF line endings",
			in:    "id_allocation:\r\n  counters:\r\n    task: 9\r\n",
			keys:  []string{"id_allocation", "counters", "story"},
			value: "4",
			want:  "id_allocation:\r\n  counters:\r\n    task: 9\r\n    story: 4\r\n",
		},
		{
			name:  "leaves a trailing comment in place",
			in:    "counters:\n  task: 223 # bumped by hand\n",
			keys:  []string{"counters", "task"},
			value: "224",
			want:  "counters:\n  task: 224 # bumped by hand\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := setYAMLPath([]byte(tc.in), tc.keys, tc.value)
			if err != nil {
				t.Fatalf("setYAMLPath: %v", err)
			}
			if string(got) != tc.want {
				t.Errorf("got:\n%s\nwant:\n%s", got, tc.want)
			}
		})
	}
}

func TestSetYAMLPathFallsBackToTheNodeTree(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		keys []string
	}{
		{
			name: "flow-style parent",
			in:   "id_allocation: {counters: {task: 9}}\n",
			keys: []string{"id_allocation", "counters", "task"},
		},
		{
			name: "block scalar at the insertion point",
			in:   "id_allocation:\n  counters:\n    task: 9\n  note: |\n    free text\n",
			keys: []string{"id_allocation", "redirects", "ACME-US-0001"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := setYAMLPath([]byte(tc.in), tc.keys, "ACME-US-0002")
			if err != nil {
				t.Fatalf("setYAMLPath: %v", err)
			}
			var doc yaml.Node
			if err := yaml.Unmarshal(got, &doc); err != nil {
				t.Fatalf("the fallback wrote invalid YAML: %v\n%s", err, got)
			}
			n, ok := lookupYAMLPath(documentMapping(&doc), tc.keys)
			if !ok || n.Value != "ACME-US-0002" {
				t.Errorf("the fallback lost the write:\n%s", got)
			}
		})
	}
}

func TestSetYAMLPathUnchangedValue(t *testing.T) {
	t.Parallel()
	got, err := setYAMLPath([]byte("counters:\n  task: 9\n"), []string{"counters", "task"}, "9")
	if err != nil || got != nil {
		t.Errorf("setYAMLPath = %q, %v; want nil, nil", got, err)
	}
}

func TestSpliceYAMLBlock(t *testing.T) {
	t.Parallel()

	const rest = "labels:\n" +
		"  - { name: core,     description: Shared Go core (model, parser, index) }\n"
	const block = "url: https://yt.example.com\nproject: ACME # short name\n"
	cases := []struct {
		name string
		in   string
		want string // empty: the splice must refuse
	}{
		{
			name: "replaces an existing block and its indented comments only",
			in: rest + "integrations:\n  # the tracker\n  youtrack:   # linked\n    url: old\n    # stale\n" +
				"  other:  { on: yes }\n\n# next\nkey: ACME\n",
			want: rest + "integrations:\n  # the tracker\n  youtrack: # linked\n    url: https://yt.example.com\n" +
				"    project: ACME # short name\n  other:  { on: yes }\n\n# next\nkey: ACME\n",
		},
		{
			name: "adds a missing block after the last entry of its parent",
			in:   "integrations:\n  other:  1\n" + rest,
			want: "integrations:\n  other:  1\n  youtrack:\n    url: https://yt.example.com\n    project: ACME # short name\n" + rest,
		},
		{
			name: "appends a missing top-level section at the end",
			in:   "key: ACME\n" + rest,
			want: "key: ACME\n" + rest + "integrations:\n  youtrack:\n    url: https://yt.example.com\n    project: ACME # short name\n",
		},
		{
			name: "keeps CRLF line endings",
			in:   "key: ACME\r\nintegrations:\r\n  youtrack:\r\n    url: old\r\n",
			want: "key: ACME\r\nintegrations:\r\n  youtrack:\r\n    url: https://yt.example.com\r\n    project: ACME # short name\r\n",
		},
		{
			name: "refuses a flow-style parent",
			in:   "key: ACME\nintegrations: { other: 1 }\n",
		},
		{
			name: "refuses a parent that is not a mapping",
			in:   "key: ACME\nintegrations: none\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var value yaml.Node
			if err := yaml.Unmarshal([]byte(block), &value); err != nil {
				t.Fatalf("parse the value: %v", err)
			}
			got, ok := SpliceYAMLBlock([]byte(tc.in), []string{"integrations", "youtrack"}, value.Content[0])
			if tc.want == "" {
				if ok {
					t.Fatalf("the splice accepted a shape it must refuse:\n%s", got)
				}
				return
			}
			if !ok {
				t.Fatal("the splice refused")
			}
			if string(got) != tc.want {
				t.Errorf("got:\n%s\nwant:\n%s", got, tc.want)
			}
		})
	}
}
