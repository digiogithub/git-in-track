package server

import (
	"path/filepath"
	"time"

	"github.com/digiogithub/git-in-track/internal/impact"
	"github.com/digiogithub/git-in-track/internal/pando"
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
	_ vault.RequirementImpact   = (*impact.Resolver)(nil)
	_ impact.CallGraph          = (*pando.Client)(nil)
)

// installTraceSeams hands every mounted vault a requirement trace engine over
// its repository's working tree, and a coverage backend over that engine, the
// test-result cache `gintrack spec ingest` fills and the repository's git
// history (GIT-US-0116), and — where the repository has git history — an
// impact resolver over the same engine and coverage, with Pando's call graph
// and semantic search read at call time (GIT-US-0119). The engine scans lazily, on the first "trace.*" or
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
		backend, hasGit := s.git.backendFor(m.id)
		if hasGit {
			changes = trace.GitChanges{Backend: backend}
		}
		coverage := trace.NewCoverage(engine, evidence, changes)
		m.vlt.SetRequirementCoverage(coverage)
		if !hasGit {
			// No history, no diff: "impact.query" answers unavailable.
			continue
		}
		m.vlt.SetRequirementImpact(impact.New(impact.Options{
			Engine: engine, Differ: backend, Coverage: coverage,
			ProjectID: codeProjectID(m),
			CallGraph: s.impactCallGraph,
			Semantic:  s.impactSemantic,
		}))
	}
}

// impactCallGraph is the Pando client tier 2 of the impact query calls, read
// at call time because a settings change rebuilds the client. Nil when no
// Pando is configured, or when the configured client cannot read the code
// graph.
func (s *Server) impactCallGraph() impact.CallGraph {
	if s.search == nil {
		return nil
	}
	if g, ok := s.search.pando().(impact.CallGraph); ok && g != nil {
		return g
	}
	return nil
}

// impactSemantic is the semantic searcher tier 3 of the impact query asks
// for requirement blocks, read at call time for the same reason. Nil when
// semantic search is off.
func (s *Server) impactSemantic() vault.SemanticSearcher {
	if s.search == nil {
		return nil
	}
	if p := s.search.semantic(); p != nil {
		return p
	}
	return nil
}
