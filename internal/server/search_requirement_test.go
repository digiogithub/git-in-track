package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/digiogithub/git-in-track/internal/pando"
	"github.com/digiogithub/git-in-track/internal/vault"
)

// specSearchBody is a spec with an introduction and two requirement blocks.
const specSearchBody = "## Purpose\n\nWhy the checkout cares about addresses.\n\n## Requirements\n\n" +
	"### DEMO-SP-0001.R1 — Trim input\n\nThe checkout SHALL trim pasted addresses.\n\n" +
	"#### Scenario: trailing spaces\n- **WHEN** an address ends in spaces\n- **THEN** they are removed\n\n" +
	"### DEMO-SP-0001.R2 — Reject empty input\n\nThe checkout SHALL refuse an empty address.\n"

// specHitPath is the path Pando reports for the spec, relative to its KBPath.
// Only the id in the file name is read, so the slug does not matter.
const specHitPath = ".pmngr/specs/DEMO-SP-0001-checkout-addresses.md"

// withSpec creates DEMO-SP-0001 through the core contract, the way the web app
// and the MCP tools do, and returns the bytes of the file it wrote.
func withSpec(t *testing.T, s *Server, root string) (string, []byte) {
	t.Helper()

	params, err := json.Marshal(map[string]any{
		"project": "DEMO", "type": "spec", "title": "Checkout addresses", "body": specSearchBody,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.repos.workspace().Dispatch(t.Context(), "item.create", params); err != nil {
		t.Fatalf("create the spec: %v", err)
	}
	matches, err := filepath.Glob(filepath.Join(root, "docs", ".pmngr", "specs", "DEMO-SP-0001-*.md"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("the spec file is not on disk: %v %v", matches, err)
	}
	data, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	return matches[0], data
}

// semanticFor runs search.semantic on the core contract.
func semanticFor(t *testing.T, s *Server, q vault.SemanticQuery) []vault.SearchHit {
	t.Helper()

	params, err := json.Marshal(q)
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.repos.workspace().Dispatch(t.Context(), "search.semantic", params)
	if err != nil {
		t.Fatalf("search.semantic: %v", err)
	}
	hits, ok := result.([]vault.SearchHit)
	if !ok {
		t.Fatalf("search.semantic answered %T", result)
	}
	return hits
}

// TestSemanticSpecHitsResolveToRequirements pins GIT-US-0118: a chunk of a spec
// file resolves to the requirement block it landed in, as a row of its own
// with its ref, spec and anchor, and a chunk outside every block stays the
// spec's.
func TestSemanticSpecHitsResolveToRequirements(t *testing.T) {
	t.Parallel()

	s, root := newAPIServer(t)
	specPath, before := withSpec(t, s, root)
	fake := &fakePando{hits: []pando.KBHit{
		{FilePath: specHitPath, Chunk: "The checkout SHALL refuse\nan empty address.", Score: 0.04, Rank: 1},
		// The same block again, from a chunk that straddles R1 and R2 but
		// overlaps R1 most: it collapses onto R1's row.
		{FilePath: specHitPath, Chunk: "The checkout SHALL trim pasted addresses.\n\n#### Scenario: trailing spaces\n" +
			"- **WHEN** an address ends in spaces\n- **THEN** they are removed\n\n### DEMO-SP-0001.R2", Score: 0.03, Rank: 2},
		{FilePath: specHitPath, Chunk: "Why the checkout cares about addresses.", Score: 0.02, Rank: 3},
	}}
	installPando(t, s, fake)

	tests := []struct {
		name string
		kind string
		want []string // ids in rank order
	}{
		{name: "every kind", want: []string{"DEMO-SP-0001.R2", "DEMO-SP-0001.R1", "DEMO-SP-0001"}},
		{name: "requirements only", kind: "requirement", want: []string{"DEMO-SP-0001.R2", "DEMO-SP-0001.R1"}},
		// A query scoped to items keeps the spec whole: every chunk is the
		// spec's, merged onto one row.
		{name: "items keep the spec", kind: "item", want: []string{"DEMO-SP-0001"}},
		{name: "pages exclude the spec", kind: "page", want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hits := semanticFor(t, s, vault.SemanticQuery{Q: "empty address", Limit: 10, Kind: tt.kind})
			var ids []string
			for _, h := range hits {
				ids = append(ids, h.ID)
			}
			if len(ids) != len(tt.want) {
				t.Fatalf("ids = %v, want %v", ids, tt.want)
			}
			for i := range ids {
				if ids[i] != tt.want[i] {
					t.Fatalf("ids = %v, want %v", ids, tt.want)
				}
			}
		})
	}

	hits := semanticFor(t, s, vault.SemanticQuery{Q: "empty address", Limit: 10})
	r2 := hits[0]
	if r2.Kind != "requirement" || r2.Spec != "DEMO-SP-0001" || r2.Anchor != "demo-sp-0001-r2" ||
		r2.Title != "Reject empty input" || r2.Project != "DEMO" || r2.Status == "" ||
		r2.Source != "pando" || r2.Index != "kb" || r2.VaultID != testRepoID {
		t.Errorf("the requirement hit does not carry its ref, spec and anchor: %+v", r2)
	}
	if r2.Path != "docs/.pmngr/specs/"+filepath.Base(specPath) {
		t.Errorf("path = %q, want the spec file", r2.Path)
	}
	if r1 := hits[1]; r1.Kind != "requirement" || r1.Anchor != "demo-sp-0001-r1" ||
		!bytes.Contains([]byte(r1.Snippet), []byte("trim pasted")) ||
		bytes.Contains([]byte(r1.Snippet), []byte("DEMO-SP-0001.R2")) {
		t.Errorf("the straddling chunk is not clipped to R1: %+v", r1)
	}
	if spec := hits[2]; spec.Kind != "item" || spec.Anchor != "" || spec.Spec != "" {
		t.Errorf("a chunk of the introduction is not the spec's own row: %+v", spec)
	}

	// The workspace search carries the same rows, with the anchor.
	var got struct {
		Hits []struct {
			Kind   string `json:"kind"`
			ID     string `json:"id"`
			Spec   string `json:"spec"`
			Anchor string `json:"anchor"`
		} `json:"hits"`
	}
	decode(t, send(t, s, request{method: http.MethodGet, target: "/api/v1/search?q=zzqx&limit=20"}), http.StatusOK, &got)
	found := false
	for _, h := range got.Hits {
		if h.Kind == "requirement" && h.ID == "DEMO-SP-0001.R2" && h.Spec == "DEMO-SP-0001" && h.Anchor == "demo-sp-0001-r2" {
			found = true
		}
	}
	if !found {
		t.Errorf("the workspace search has no requirement row: %+v", got.Hits)
	}

	// Pando is only ever read: the spec file is byte for byte what the create
	// wrote.
	after, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Error("a semantic search changed the spec file")
	}
}

// A block removed since Pando's last pass is not a ghost requirement: its
// chunk is found nowhere in the current body, so the hit stays the spec's.
func TestSemanticSpecHitOnARemovedBlockStaysTheSpec(t *testing.T) {
	t.Parallel()

	s, root := newAPIServer(t)
	withSpec(t, s, root)
	installPando(t, s, &fakePando{hits: []pando.KBHit{
		{FilePath: specHitPath, Chunk: "The checkout SHALL geocode every address it stores.", Score: 0.04, Rank: 1},
	}})
	hits := semanticFor(t, s, vault.SemanticQuery{Q: "geocode", Limit: 5})
	if len(hits) != 1 || hits[0].Kind != "item" || hits[0].ID != "DEMO-SP-0001" {
		t.Fatalf("hits = %+v, want the spec itself", hits)
	}
}
