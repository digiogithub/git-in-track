package server

import (
	"strings"
	"testing"

	"github.com/digiogithub/git-in-track/internal/config"
)

// TestTraceSeamInstalled checks that the companion hands every repository a
// requirement tracer, so "trace.*" answers rather than failing `unavailable`
// as it does in browser-only mode.
func TestTraceSeamInstalled(t *testing.T) {
	t.Parallel()
	s, _ := newAPIServer(t)
	mounts := s.repos.ready()
	if len(mounts) == 0 {
		t.Fatal("no mounted repository")
	}
	for _, m := range mounts {
		if !m.vlt.TraceAvailable() {
			t.Errorf("repository %s has no requirement tracer", m.id)
		}
		out := m.vlt.Call("trace.touching", `{"changes":[{"path":"README.md"}]}`)
		if !strings.Contains(out, `"hits":[]`) {
			t.Errorf("trace.touching on %s = %s, want no hits", m.id, out)
		}
		if !m.vlt.CoverageAvailable() {
			t.Errorf("repository %s has no coverage backend", m.id)
		}
		if out := m.vlt.Call("coverage.list", `{}`); !strings.Contains(out, `"ok":true`) {
			t.Errorf("coverage.list on %s = %s", m.id, out)
		}
	}
}

// TestImpactSeamInstalled checks that a repository with git history gets an
// impact resolver — tier 1 answers, and tiers 2 and 3 say `unavailable`
// without Pando — while one without history has none.
func TestImpactSeamInstalled(t *testing.T) {
	t.Parallel()
	s, _ := newGitServer(t, config.Git{})
	for _, m := range s.repos.ready() {
		if !m.vlt.ImpactAvailable() {
			t.Fatalf("repository %s has no impact resolver", m.id)
		}
		out := m.vlt.Call("impact.query", `{"base":"HEAD"}`)
		if !strings.Contains(out, `"ok":true`) ||
			!strings.Contains(out, `{"tier":1,"status":"ok","hits":0}`) ||
			!strings.Contains(out, `{"tier":2,"status":"unavailable"`) ||
			!strings.Contains(out, `{"tier":3,"status":"unavailable"`) {
			t.Errorf("impact.query on %s = %s", m.id, out)
		}
	}
	plain, _ := newAPIServer(t)
	for _, m := range plain.repos.ready() {
		if _, hasGit := plain.git.backendFor(m.id); hasGit {
			continue
		}
		if m.vlt.ImpactAvailable() {
			t.Errorf("repository %s without git has an impact resolver", m.id)
		}
		if out := m.vlt.Call("impact.query", `{}`); !strings.Contains(out, `"unavailable"`) {
			t.Errorf("impact.query without git = %s, want unavailable", out)
		}
	}
}
