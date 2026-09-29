package config

import (
	"errors"
	"strings"
	"testing"
)

func lookFound(name string) (string, error) { return "/usr/bin/" + name, nil }
func lookMissing(string) (string, error)    { return "", errors.New("not found") }

func TestResolvePandoMode(t *testing.T) {
	tests := []struct {
		name        string
		p           SearchPando
		look        func(string) (string, error)
		mode        string
		rule        int
		unavailable bool
		contains    string
	}{
		{"rule 1 off beats everything", SearchPando{Mode: "off", MCPURL: "http://127.0.0.1:1/mcp"}, lookFound, "off", 1, false, "turned off"},
		{"rule 2 external without url", SearchPando{Mode: "external"}, lookFound, "external", 2, false, "not configured"},
		{"rule 2 external with url", SearchPando{Mode: "external", MCPURL: "http://127.0.0.1:1/mcp"}, lookMissing, "external", 2, false, ""},
		{"rule 3 managed with binary", SearchPando{Mode: "managed"}, lookFound, "managed", 3, false, ""},
		{"rule 3 managed without binary", SearchPando{Mode: "managed"}, lookMissing, "managed", 3, true, "no pando binary"},
		{"rule 4 auto with url beats binary", SearchPando{MCPURL: "http://127.0.0.1:1/mcp"}, lookFound, "external", 4, false, ""},
		{"rule 4 explicit auto", SearchPando{Mode: "auto", MCPURL: "http://127.0.0.1:1/mcp"}, lookMissing, "external", 4, false, ""},
		{"rule 5 auto with binary", SearchPando{}, lookFound, "managed", 5, false, ""},
		{"rule 6 auto without binary", SearchPando{Mode: "auto"}, lookMissing, "off", 6, false, "no Pando binary"},
		{"rule 6 nil lookup", SearchPando{}, nil, "off", 6, false, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ResolvePandoMode(tc.p, tc.look)
			if got.Mode != tc.mode || got.Rule != tc.rule || got.Unavailable != tc.unavailable {
				t.Fatalf("got %+v, want mode %s rule %d unavailable %v", got, tc.mode, tc.rule, tc.unavailable)
			}
			if !strings.Contains(got.Reason, tc.contains) {
				t.Fatalf("reason %q lacks %q", got.Reason, tc.contains)
			}
		})
	}

	t.Run("looks up the configured binary", func(t *testing.T) {
		var asked string
		ResolvePandoMode(SearchPando{Managed: PandoManaged{Binary: "/opt/pando"}}, func(n string) (string, error) {
			asked = n
			return n, nil
		})
		if asked != "/opt/pando" {
			t.Fatalf("looked up %q", asked)
		}
	})
}

func TestValidatePandoMode(t *testing.T) {
	tests := []struct {
		name   string
		yaml   string
		fields []string
	}{
		{"defaults", "version: 1\n", nil},
		{"all keys", "version: 1\nsearch:\n  pando:\n    mode: managed\n    managed:\n      binary: /opt/pando\n      maxInstances: 2\n      minVersion: v1.1.0\n      logLevel: warn\nrepos: []\n", nil},
		{"unknown mode", "version: 1\nsearch:\n  pando:\n    mode: hybrid\n", []string{"search.pando.mode"}},
		{"managed with external keys", "version: 1\nsearch:\n  pando:\n    mode: managed\n    mcpUrl: http://127.0.0.1:9/mcp\n    mcpToken: x\n    restUrl: http://127.0.0.1:9\n    restToken: y\n    projectId: p\n",
			[]string{"search.pando.mcpUrl", "search.pando.mcpToken", "search.pando.restUrl", "search.pando.restToken", "search.pando.projectId"}},
		{"auto with external keys is fine", "version: 1\nsearch:\n  pando:\n    mcpUrl: http://127.0.0.1:9/mcp\n", nil},
		{"maxInstances too big", "version: 1\nsearch:\n  pando:\n    managed:\n      maxInstances: 65\n", []string{"search.pando.managed.maxInstances"}},
		{"maxInstances negative", "version: 1\nsearch:\n  pando:\n    managed:\n      maxInstances: -1\n", []string{"search.pando.managed.maxInstances"}},
		{"bad log level", "version: 1\nsearch:\n  pando:\n    managed:\n      logLevel: loud\n", []string{"search.pando.managed.logLevel"}},
		{"bad version", "version: 1\nsearch:\n  pando:\n    managed:\n      minVersion: latest\n", []string{"search.pando.managed.minVersion"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := Parse([]byte(tc.yaml))
			if err == nil {
				err = cfg.Validate()
			}
			if len(tc.fields) == 0 {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			var fes FieldErrors
			if !errors.As(err, &fes) {
				t.Fatalf("want FieldErrors, got %v", err)
			}
			if len(fes) != len(tc.fields) {
				t.Fatalf("got %v, want fields %v", fes, tc.fields)
			}
			got := map[string]bool{}
			for _, fe := range fes {
				got[fe.Field] = true
			}
			for _, f := range tc.fields {
				if !got[f] {
					t.Fatalf("no error names %q in %v", f, fes)
				}
			}
		})
	}

	t.Run("repos semanticSearch round-trips", func(t *testing.T) {
		cfg, err := Parse([]byte("version: 1\nrepos:\n  - id: a\n    path: /tmp/a\n    role: project\n    semanticSearch: true\n"))
		if err != nil {
			t.Fatal(err)
		}
		if !cfg.Repos[0].SemanticSearch {
			t.Fatal("semanticSearch was not parsed")
		}
	})
}
