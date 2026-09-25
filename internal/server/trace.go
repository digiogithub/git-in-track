package server

import (
	"time"

	"github.com/digiogithub/git-in-track/internal/trace"
	"github.com/digiogithub/git-in-track/internal/vault"
)

// traceMaxAge bounds how stale the marker scan behind the requirement trace
// graph may be. The watcher covers only the docs folders, so code edited by
// hand is picked up by a full rescan on the first trace query after this age;
// a diff-driven query rescans its own paths immediately (GIT-US-0114).
const traceMaxAge = 30 * time.Second

// The trace engine is the vault's requirement tracer.
var _ vault.RequirementTracer = (*trace.Engine)(nil)

// installTraceSeams hands every mounted vault a requirement trace engine over
// its repository's working tree. The engine scans lazily, on the first
// "trace.*" call, so a companion that never asks pays nothing.
func (s *Server) installTraceSeams(now func() time.Time) {
	for _, m := range s.repos.ready() {
		m.vlt.SetRequirementTracer(trace.NewEngine(m.path, trace.EngineOptions{MaxAge: traceMaxAge, Now: now}))
	}
}
