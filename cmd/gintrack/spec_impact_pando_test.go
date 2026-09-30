package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/digiogithub/git-in-track/internal/config"
	"github.com/digiogithub/git-in-track/internal/core"
	"github.com/digiogithub/git-in-track/internal/impact"
)

// touchNextID edits NextID, which ACME-SP-0001.R1 traces, so the diff against
// HEAD has one changed symbol for tier 2 to ask Pando about.
func touchNextID(t *testing.T, root string) {
	t.Helper()
	src := filepath.Join(root, "src", "alloc.go")
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte(strings.Replace(string(data), "return 1", "return 1 + 0", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
}

// tierStatus returns the status of one tier of a report, "" when absent.
func tierStatus(tiers []core.ImpactTier, tier int) core.ImpactTierStatus {
	for _, tr := range tiers {
		if tr.Tier == tier {
			return tr.Status
		}
	}
	return ""
}

// TestSpecImpactPandoTiers is the CLI half of GIT-US-0147: with
// `search.pando` configured, `gintrack spec impact` hands the impact seam the
// same Pando client and semantic searcher `gintrack serve` builds, so tiers 2
// and 3 answer; without it they report unavailable while tier 1 answers.
func TestSpecImpactPandoTiers(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	tests := []struct {
		name  string
		pando bool
		want  core.ImpactTierStatus
	}{
		{name: "pando configured: tiers 2 and 3 answer", pando: true, want: core.ImpactTierOK},
		{name: "no pando: tiers 2 and 3 unavailable", pando: false, want: core.ImpactTierUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			root := gitSpecRepo(t, h)
			touchNextID(t, root)
			var fake *fakeSemanticPando
			if tt.pando {
				fake = newFakeSemanticPando(t)
				fake.usePando(t, h.Config)
			}

			got := decode[specImpactPayload](t, h.mustRun("spec", "impact", "--since", "HEAD", "--json"))
			if s := tierStatus(got.Report.Tiers, core.ImpactTierDirect); s != core.ImpactTierOK {
				t.Errorf("tier 1 = %q, want ok", s)
			}
			for _, tier := range []int{core.ImpactTierTransitive, core.ImpactTierSemantic} {
				if s := tierStatus(got.Report.Tiers, tier); s != tt.want {
					t.Errorf("tier %d = %q, want %q (%+v)", tier, s, tt.want, got.Report.Tiers)
				}
			}
			if tt.pando && fake.called("code_impact_analysis") == 0 {
				t.Error("tier 2 never called code_impact_analysis on the configured Pando")
			}
			if tt.pando && fake.called("code_find_symbol") == 0 {
				t.Error("tier 2 never pinned a changed name with code_find_symbol (GIT-US-0166)")
			}
			if tt.pando && fake.called("code_related_files") == 0 {
				t.Error("tier 2 never checked that a no-callers answer came from a code graph (GIT-US-0167)")
			}
		})
	}
}

// TestSpecImpactPandoNoCallEdges is GIT-US-0167 over the CLI: a Pando whose
// project was indexed with [TokenOptimization] BuildCodeGraph = false answers
// "No callers found" for every name and relates no file, and tier 2 reports
// that as unavailable, naming BuildCodeGraph, never as ok with no hits.
func TestSpecImpactPandoNoCallEdges(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	h := newHarness(t)
	root := gitSpecRepo(t, h)
	touchNextID(t, root)
	fake := newFakeSemanticPando(t)
	fake.noCallEdges.Store(true)
	fake.usePando(t, h.Config)

	got := decode[specImpactPayload](t, h.mustRun("spec", "impact", "--since", "HEAD", "--json"))
	if s := tierStatus(got.Report.Tiers, core.ImpactTierDirect); s != core.ImpactTierOK {
		t.Errorf("tier 1 = %q, want ok", s)
	}
	for _, tr := range got.Report.Tiers {
		if tr.Tier != core.ImpactTierTransitive {
			continue
		}
		if tr.Status != core.ImpactTierUnavailable || tr.Message != impact.NoCallEdges {
			t.Errorf("tier 2 = %+v, want unavailable with %q", tr, impact.NoCallEdges)
		}
	}
	if fake.called("code_related_files") == 0 {
		t.Error("tier 2 never probed the code graph with code_related_files")
	}
}

// TestMCPStdioSpecImpactPando is the stdio half of GIT-US-0147: spec_impact
// over a spawned `gintrack mcp` reaches the configured Pando for tiers 2 and 3.
func TestMCPStdioSpecImpactPando(t *testing.T) {
	if testing.Short() {
		t.Skip("building the binary is too slow for -short")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	binary := buildGintrack(t)
	h := newHarness(t)
	root := gitSpecRepo(t, h)
	touchNextID(t, root)
	fake := newFakeSemanticPando(t)
	fake.usePando(t, h.Config)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "mcp") //nolint:gosec // the path is one this test built
	cmd.Env = append(os.Environ(), "GINTRACK_CONFIG="+h.Config)
	cmd.Stderr = os.Stderr
	client := sdk.NewClient(&sdk.Implementation{Name: "test-client", Version: "0.0.1"}, nil)
	session, err := client.Connect(ctx, &sdk.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatalf("connect to `gintrack mcp` over stdio: %v", err)
	}
	defer func() { _ = session.Close() }()

	got := callStdio[struct {
		Tiers []core.ImpactTier `json:"tiers"`
	}](ctx, t, session, "spec_impact", map[string]any{"base": "HEAD"})
	for _, tier := range []int{core.ImpactTierDirect, core.ImpactTierTransitive, core.ImpactTierSemantic} {
		if s := tierStatus(got.Tiers, tier); s != core.ImpactTierOK {
			t.Errorf("tier %d = %q, want ok (%+v)", tier, s, got.Tiers)
		}
	}
	if fake.called("code_impact_analysis") == 0 {
		t.Error("tier 2 never called code_impact_analysis on the configured Pando")
	}
}

// TestSpecImpactTier3Query is the fake-Pando half of GIT-US-0165: tier 3 asks
// kb_search_documents for the specs folder alone (path_prefix), with a query
// built from the story and the changed declaration in words; the fake finds
// nothing under the prefix, so the search is asked again, once, unfiltered.
// The code index never holds a spec, so code_hybrid_search is not called.
func TestSpecImpactTier3Query(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	h := newHarness(t)
	root := gitSpecRepo(t, h)
	touchNextID(t, root)
	fake := newFakeSemanticPando(t)
	fake.usePando(t, h.Config)

	got := decode[specImpactPayload](t, h.mustRun("spec", "impact", "--since", "HEAD",
		"--story", "ACME-US-0001", "--tiers", "3", "--json"))
	if s := tierStatus(got.Report.Tiers, core.ImpactTierSemantic); s != core.ImpactTierOK {
		t.Fatalf("tier 3 = %q, want ok (%+v)", s, got.Report.Tiers)
	}
	calls := fake.kbSearches()
	// The long query and the name-word query (GIT-US-0180), each prefixed and
	// then retried unfiltered.
	if len(calls) != 4 {
		t.Fatalf("kb_search_documents calls = %v, want the prefixed one and its unfiltered retry, twice", calls)
	}
	if p, _ := calls[0]["path_prefix"].(string); p != ".pmngr/specs/" {
		t.Errorf("first call path_prefix = %q, want .pmngr/specs/", p)
	}
	if _, ok := calls[1]["path_prefix"]; ok {
		t.Errorf("the retry still carries a path_prefix: %v", calls[1])
	}
	if q, _ := calls[0]["query"].(string); q != "Story ACME-US-0001\nnext id" {
		t.Errorf("query = %q, want the story title and the changed declaration in words", q)
	}
	if q, _ := calls[2]["query"].(string); q != "next id" {
		t.Errorf("name query = %q, want the changed declaration in words alone", q)
	}
	if n := fake.called("code_hybrid_search"); n != 0 {
		t.Errorf("code_hybrid_search called %d times for a requirement query", n)
	}
}

// externalGraphFiles lists the persisted graph checks of external Pando
// endpoints under the configuration's cache directory.
func externalGraphFiles(t *testing.T, configPath string) []string {
	t.Helper()
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("load the configuration: %v", err)
	}
	files, err := filepath.Glob(filepath.Join(cfg.CacheDir(configPath), "pando", "external", "graph-probe-*.json"))
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// TestSpecImpactPersistsGraphProbe is GIT-US-0188: a short-lived
// `gintrack spec impact` run against an external Pando reuses the graph check
// an earlier run persisted, so it does not pay for the slow probe again.
func TestSpecImpactPersistsGraphProbe(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	run := func(h *harness) specImpactPayload {
		return decode[specImpactPayload](t, h.mustRun("spec", "impact", "--since", "HEAD", "--json"))
	}
	setup := func(t *testing.T) (*harness, *fakeSemanticPando) {
		h := newHarness(t)
		touchNextID(t, gitSpecRepo(t, h))
		fake := newFakeSemanticPando(t)
		fake.usePando(t, h.Config)
		return h, fake
	}

	// Verifies: GIT-US-0188
	t.Run("a stored answer is reused by the next run", func(t *testing.T) {
		h, fake := setup(t)
		first := run(h)
		if s := tierStatus(first.Report.Tiers, core.ImpactTierTransitive); s != core.ImpactTierOK {
			t.Fatalf("first run tier 2 = %q, want ok", s)
		}
		probes := fake.called("code_related_files")
		if probes == 0 {
			t.Fatal("the first run never probed the graph")
		}
		if len(externalGraphFiles(t, h.Config)) != 1 {
			t.Fatalf("stored files = %v, want one", externalGraphFiles(t, h.Config))
		}
		second := run(h)
		if s := tierStatus(second.Report.Tiers, core.ImpactTierTransitive); s != core.ImpactTierOK {
			t.Errorf("second run tier 2 = %q, want ok", s)
		}
		if got := fake.called("code_related_files"); got != probes {
			t.Errorf("the second run probed again: %d calls, want %d", got, probes)
		}
	})

	t.Run("an expired answer is probed again", func(t *testing.T) {
		h, fake := setup(t)
		run(h)
		probes := fake.called("code_related_files")
		files := externalGraphFiles(t, h.Config)
		if len(files) != 1 {
			t.Fatalf("stored files = %v, want one", files)
		}
		b, err := os.ReadFile(files[0])
		if err != nil {
			t.Fatal(err)
		}
		var rec map[string]any
		if err := json.Unmarshal(b, &rec); err != nil {
			t.Fatal(err)
		}
		rec["expires"] = time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano)
		b, _ = json.Marshal(rec)
		if err := os.WriteFile(files[0], b, 0o600); err != nil {
			t.Fatal(err)
		}
		run(h)
		if got := fake.called("code_related_files"); got <= probes {
			t.Errorf("an expired answer was reused: %d calls, still %d", got, probes)
		}
	})

	t.Run("another endpoint does not reuse the answer", func(t *testing.T) {
		h, fake := setup(t)
		run(h)
		other := newFakeSemanticPando(t)
		other.usePando(t, h.Config)
		run(h)
		if other.called("code_related_files") == 0 {
			t.Error("a different endpoint reused the answer of the first")
		}
		if got := len(externalGraphFiles(t, h.Config)); got != 2 {
			t.Errorf("stored files = %d, want one per endpoint", got)
		}
		_ = fake
	})
}
