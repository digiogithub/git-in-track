package vault

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/digiogithub/git-in-track/internal/core"
)

// reentrantSeams is every requirement seam at once, and each of its answers
// first calls back into the vault the way the companion's backends may: the
// Pando semantic searcher resolves its hits through Requirement and Item.
type reentrantSeams struct {
	v *Vault
}

func (s reentrantSeams) reenter(ix *core.Index) error {
	ref := core.RequirementRef{Spec: "DEMO-SP-0001", Number: 1}
	if _, ok := s.v.Requirement(ref); !ok {
		return fmt.Errorf("%s does not resolve", ref)
	}
	if _, ok := s.v.Item("DEMO-US-0001"); !ok {
		return fmt.Errorf("DEMO-US-0001 does not resolve")
	}
	_ = s.v.Stats()
	// The index handed to the seam is read without the vault mutex.
	if _, err := ix.Requirements(core.RequirementFilter{}); err != nil {
		return err
	}
	return nil
}

func (s reentrantSeams) TraceRequirement(_ context.Context, ix *core.Index, ref core.RequirementRef) (core.TracedRequirement, error) {
	if err := s.reenter(ix); err != nil {
		return core.TracedRequirement{}, err
	}
	return core.TracedRequirement{Ref: ref, Code: []core.TraceEdge{}, Tests: []core.TraceEdge{}, Work: []core.TraceWork{}}, nil
}

func (s reentrantSeams) TraceTouching(_ context.Context, ix *core.Index, _ []core.TraceChange) ([]core.TraceHit, error) {
	return nil, s.reenter(ix)
}

func (s reentrantSeams) Coverage(_ context.Context, ix *core.Index, reqs []core.RequirementView) ([]core.CoverageRow, error) {
	if err := s.reenter(ix); err != nil {
		return nil, err
	}
	rows := make([]core.CoverageRow, 0, len(reqs))
	for _, r := range reqs {
		rows = append(rows, core.CoverageRow{Ref: r.Ref, Status: core.CoverageUntested})
	}
	return rows, nil
}

// StampEvidence runs under the vault mutex, inside the stamp's write
// transaction, so it must not call back into the vault.
func (s reentrantSeams) StampEvidence(_ context.Context, _ *core.Index, reqs []core.RequirementView) ([]core.StampEvidence, error) {
	return make([]core.StampEvidence, len(reqs)), nil
}

func (s reentrantSeams) Impact(_ context.Context, ix *core.Index, q core.ImpactQuery) (core.ImpactResult, error) {
	if err := s.reenter(ix); err != nil {
		return core.ImpactResult{}, err
	}
	return core.ImpactResult{Base: q.Base}, nil
}

// reentrantVault is a spec vault with a story, every requirement seam
// installed as reentrantSeams.
func reentrantVault(t *testing.T) *Vault {
	t.Helper()
	v := specVault(t)
	seams := reentrantSeams{v: v}
	v.SetRequirementTracer(seams)
	v.SetRequirementCoverage(seams)
	v.SetRequirementImpact(seams)
	return v
}

// TestSeamsMayReenterTheVault is the regression test of GIT-US-0163: every
// read answered by a host-installed seam releases the vault mutex before the
// seam runs, so a seam that calls back into the vault answers instead of
// deadlocking.
func TestSeamsMayReenterTheVault(t *testing.T) {
	t.Parallel()
	tests := []struct {
		method string
		params map[string]any
	}{
		{"trace.requirement", map[string]any{"ref": "DEMO-SP-0001.R1"}},
		{"trace.touching", map[string]any{"changes": []any{map[string]any{"path": "a.go"}}}},
		{"coverage.list", map[string]any{}},
		{"coverage.list", map[string]any{"refs": []string{"DEMO-SP-0001.R1"}}},
		{"spec.context", map[string]any{"id": "DEMO-US-0001"}},
		{"impact.query", map[string]any{"base": "main"}},
		{"impact.report", map[string]any{"base": "main", "story": "DEMO-US-0001"}},
	}
	for _, tc := range tests {
		t.Run(tc.method, func(t *testing.T) {
			t.Parallel()
			v := reentrantVault(t)
			done := make(chan envelope, 1)
			go func() { done <- rawCall(t, v, tc.method, tc.params) }()
			select {
			case env := <-done:
				if !env.OK {
					t.Fatalf("%s failed: %s %s", tc.method, env.Error.Code, env.Error.Message)
				}
			case <-time.After(10 * time.Second):
				t.Fatalf("%s deadlocked: its seam called back into the vault and never returned", tc.method)
			}
		})
	}
}

// TestSeamReadsRaceWrites runs the seam reads while writes land, so that
// `go test -race` proves the index they read without the vault mutex is
// shared safely.
func TestSeamReadsRaceWrites(t *testing.T) {
	t.Parallel()
	v := reentrantVault(t)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 10; i++ {
			env := rawCall(t, v, "item.create", map[string]any{
				"project": "DEMO", "type": "task", "title": fmt.Sprintf("Task %d", i), "parent": "DEMO-US-0001",
			})
			if !env.OK {
				t.Errorf("item.create failed: %s %s", env.Error.Code, env.Error.Message)
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 10; i++ {
			for _, m := range []string{"impact.query", "coverage.list", "spec.context"} {
				env := rawCall(t, v, m, map[string]any{"id": "DEMO-US-0001"})
				if !env.OK {
					t.Errorf("%s failed: %s %s", m, env.Error.Code, env.Error.Message)
					return
				}
			}
		}
	}()
	wg.Wait()
}
