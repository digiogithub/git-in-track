package server

import (
	"time"

	"github.com/digiogithub/git-in-track/internal/core/osfs"
	"github.com/digiogithub/git-in-track/internal/gitops"
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

// TraceSeams describes the requirement seams of one mounted repository: what
// [InstallTraceSeams] needs to build them. The companion fills it for every
// repository it mounts, and `gintrack mcp` over stdio fills it the same way,
// so an agent gets one implementation of trace, coverage and impact whichever
// transport it speaks (GIT-US-0124).
type TraceSeams struct {
	// Root is the repository's working tree on disk. Required.
	Root string
	// Git is the repository's history. Nil means no history: coverage cannot
	// check drift, and "impact.query" answers unavailable.
	Git gitops.Backend
	// ProjectID is the Pando code project of the repository.
	ProjectID string
	// CallGraph and Semantic are read at call time by tiers 2 and 3 of the
	// impact query; nil, or a function answering nil, leaves the tier
	// unavailable while tier 1 still answers.
	CallGraph func() impact.CallGraph
	Semantic  func() vault.SemanticSearcher
	// Now is the clock of the marker scan; nil means time.Now.
	Now func() time.Time
}

// InstallTraceSeams hands a vault a requirement trace engine over its
// repository's working tree, a coverage backend over that engine, the
// verification cache of each project (<docs>/.pmngr/verify.json, GIT-US-0141)
// and the git history (GIT-US-0116), and — where the
// repository has git history — an impact resolver over the same engine and
// coverage, with Pando's call graph and semantic search read at call time
// (GIT-US-0119). The engine scans lazily, on the first "trace.*" or
// "coverage.*" call, so a host that never asks pays nothing.
func InstallTraceSeams(v *vault.Vault, o TraceSeams) {
	engine := trace.NewEngine(o.Root, trace.EngineOptions{MaxAge: traceMaxAge, Now: o.Now})
	v.SetRequirementTracer(engine)
	var evidence trace.VerifyEvidence
	if fsys, err := osfs.New(o.Root); err == nil {
		evidence.FS = fsys
	}
	var changes trace.ChangeLister
	if o.Git != nil {
		changes = trace.GitChanges{Backend: o.Git}
	}
	coverage := trace.NewCoverage(engine, evidence, changes)
	v.SetRequirementCoverage(coverage)
	if o.Git == nil {
		// No history, no diff: "impact.query" answers unavailable.
		v.SetRequirementImpact(nil)
		return
	}
	v.SetRequirementImpact(impact.New(impact.Options{
		Engine: engine, Differ: o.Git, Coverage: coverage,
		ProjectID: o.ProjectID,
		CallGraph: o.CallGraph,
		Semantic:  o.Semantic,
	}))
}

// installTraceSeams installs the requirement seams on every mounted vault.
func (s *Server) installTraceSeams(now func() time.Time) {
	for _, m := range s.repos.ready() {
		seams := TraceSeams{
			Root: m.path, Now: now,
			ProjectID: codeProjectID(m),
			CallGraph: s.impactCallGraph,
			Semantic:  s.impactSemantic,
		}
		if backend, ok := s.git.backendFor(m.id); ok {
			seams.Git = backend
		}
		InstallTraceSeams(m.vlt, seams)
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
