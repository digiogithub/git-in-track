package vault

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/digiogithub/git-in-track/internal/core"
)

// specWorkspace attaches an in-memory fixture holding DEMO-SP-0001 with R1 and
// R2.
func specWorkspace(t *testing.T) *Workspace {
	t.Helper()
	w := NewWorkspace()
	// An in-memory copy of the fixture: the test writes.
	if _, err := w.Attach("demo", RoleProject, specVault(t)); err != nil {
		t.Fatalf("attach: %v", err)
	}
	return w
}

// createRequirementIn runs requirement.create through the workspace and
// returns the created view and similar[].
func createRequirementIn(t *testing.T, w *Workspace) (core.RequirementView, []SimilarRequirement) {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{
		"spec": "DEMO-SP-0001", "title": "Trim pasted input",
		"text": "The checkout SHALL trim pasted addresses before saving them.\n\n" +
			"#### Scenario: pasted\n- **WHEN** an address is pasted\n- **THEN** its spaces are removed\n",
	})
	result, err := w.Dispatch(t.Context(), "requirement.create", raw)
	if err != nil {
		t.Fatalf("requirement.create: %v", err)
	}
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("result is %T", result)
	}
	similar, ok := m["similar"].([]SimilarRequirement)
	if !ok {
		t.Fatalf("similar is %T, want []SimilarRequirement always present", m["similar"])
	}
	return m["requirement"].(core.RequirementView), similar
}

// funcSemantic is a semantic backend defined by one function.
type funcSemantic func(ctx context.Context, q SemanticQuery) ([]core.SearchHit, error)

func (f funcSemantic) SearchSemantic(ctx context.Context, q SemanticQuery) ([]core.SearchHit, error) {
	return f(ctx, q)
}

func reqHit(ref string, score float64) core.SearchHit {
	return core.SearchHit{
		Kind: core.SearchKindRequirement, ID: core.ItemID(ref), Title: "t " + ref, Spec: "DEMO-SP-0001",
		Status: "backlog", Project: "DEMO", Score: score, Source: core.SearchSourcePando,
	}
}

func TestCreateRequirementSimilar(t *testing.T) {
	t.Parallel()

	t.Run("similar blocks are returned, ranked, capped and thresholded", func(t *testing.T) {
		t.Parallel()
		w := specWorkspace(t)
		var got SemanticQuery
		w.SetSemanticSearcher(funcSemantic(func(_ context.Context, q SemanticQuery) ([]core.SearchHit, error) {
			got = q
			return []core.SearchHit{
				reqHit("DEMO-SP-0001.R3", 0.9), // the new requirement itself
				reqHit("DEMO-SP-0001.R1", 0.03),
				{Kind: "item", ID: "DEMO-US-0001", Project: "DEMO", Score: 0.05},
				reqHit("DEMO-SP-0001.R2", 0.02),
				reqHit("DEMO-SP-0001.R1", 0.01), // a second chunk of R1
				reqHit("DEMO-SP-0009.R1", 0.018),
				reqHit("DEMO-SP-0008.R4", 0.016),
				reqHit("DEMO-SP-0007.R2", 0.01), // under half the best score
			}, nil
		}))
		view, similar := createRequirementIn(t, w)
		if view.Ref.String() != "DEMO-SP-0001.R3" {
			t.Fatalf("created %s", view.Ref)
		}
		want := []string{"DEMO-SP-0001.R1", "DEMO-SP-0001.R2", "DEMO-SP-0009.R1"}
		if len(similar) != len(want) {
			t.Fatalf("similar = %+v, want %v", similar, want)
		}
		for i, ref := range want {
			if similar[i].Ref != ref || similar[i].VaultID != "demo" || similar[i].Score <= 0 {
				t.Errorf("similar[%d] = %+v, want %s", i, similar[i], ref)
			}
		}
		if got.Kind != core.SearchKindRequirement || got.Project != "DEMO" ||
			got.Q != "Trim pasted input\nThe checkout SHALL trim pasted addresses before saving them." {
			t.Errorf("query = %+v", got)
		}
	})

	t.Run("nothing similar", func(t *testing.T) {
		t.Parallel()
		w := specWorkspace(t)
		w.SetSemanticSearcher(funcSemantic(func(context.Context, SemanticQuery) ([]core.SearchHit, error) {
			return nil, nil
		}))
		if _, similar := createRequirementIn(t, w); len(similar) != 0 {
			t.Errorf("similar = %+v", similar)
		}
	})

	t.Run("no backend: no suggestions and no error", func(t *testing.T) {
		t.Parallel()
		w := specWorkspace(t)
		if _, similar := createRequirementIn(t, w); len(similar) != 0 {
			t.Errorf("similar = %+v", similar)
		}
	})

	t.Run("a failing backend never fails the create", func(t *testing.T) {
		t.Parallel()
		w := specWorkspace(t)
		w.SetSemanticSearcher(funcSemantic(func(context.Context, SemanticQuery) ([]core.SearchHit, error) {
			return nil, errors.New("pando: connection refused")
		}))
		if _, similar := createRequirementIn(t, w); len(similar) != 0 {
			t.Errorf("similar = %+v", similar)
		}
	})

	t.Run("a slow backend is abandoned at the deadline", func(t *testing.T) {
		t.Parallel()
		w := NewWorkspace()
		release := make(chan struct{})
		t.Cleanup(func() { close(release) })
		// This backend ignores its context on purpose.
		w.SetSemanticSearcher(funcSemantic(func(context.Context, SemanticQuery) ([]core.SearchHit, error) {
			<-release
			return []core.SearchHit{reqHit("DEMO-SP-0001.R1", 1)}, nil
		}))
		ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
		defer cancel()
		start := time.Now()
		view := core.RequirementView{
			Ref: core.RequirementRef{Spec: "DEMO-SP-0001", Number: 3}, Spec: "DEMO-SP-0001", Title: "x", Statement: "y",
		}
		similar := w.SimilarRequirements(ctx, view)
		if len(similar) != 0 || similar == nil {
			t.Errorf("similar = %#v, want an empty list", similar)
		}
		if elapsed := time.Since(start); elapsed > SimilarRequirementsTimeout {
			t.Errorf("waited %s", elapsed)
		}
	})
}
