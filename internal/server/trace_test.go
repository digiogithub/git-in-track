package server

import (
	"strings"
	"testing"
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
