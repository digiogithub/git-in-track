package server

import (
	"path/filepath"
	"time"

	"github.com/digiogithub/git-in-track/internal/trace"
	"github.com/digiogithub/git-in-track/internal/vault"
)

// traceMaxAge bounds how stale the marker scan behind the requirement trace
// graph may be. The watcher covers only the docs folders, so code edited by
// hand is picked up by a full rescan on the first trace query after this age;
// a diff-driven query rescans its own paths immediately (GIT-US-0114).
const traceMaxAge = 30 * time.Second

// The trace engine is the vault's requirement tracer, and Coverage its
// coverage backend.
var (
	_ vault.RequirementTracer   = (*trace.Engine)(nil)
	_ vault.RequirementCoverage = (*trace.Coverage)(nil)
)

// installTraceSeams hands every mounted vault a requirement trace engine over
// its repository's working tree, and a coverage backend over that engine, the
// test-result cache `gintrack spec ingest` fills and the repository's git
// history (GIT-US-0116). The engine scans lazily, on the first "trace.*" or
// "coverage.*" call, so a companion that never asks pays nothing.
func (s *Server) installTraceSeams(now func() time.Time) {
	cacheDir := s.opts.SyncEngine.CacheDir
	if cacheDir == "" && s.opts.ConfigPath != "" {
		cacheDir = filepath.Dir(s.opts.ConfigPath)
	}
	for _, m := range s.repos.ready() {
		engine := trace.NewEngine(m.path, trace.EngineOptions{MaxAge: traceMaxAge, Now: now})
		m.vlt.SetRequirementTracer(engine)
		var evidence trace.ResultEvidence
		if cacheDir != "" {
			evidence.Store = trace.NewResultStore(trace.DefaultResultCachePath(cacheDir, m.path))
		}
		var changes trace.ChangeLister
		if backend, ok := s.git.backendFor(m.id); ok {
			changes = trace.GitChanges{Backend: backend}
		}
		m.vlt.SetRequirementCoverage(trace.NewCoverage(engine, evidence, changes))
	}
}
