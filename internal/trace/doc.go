// Package trace is the native marker scanner of the requirement trace engine
// (ADR-037 section 8, docs/03 section 21.7).
//
// It walks a repository, honoring .gitignore, and extracts the full-line
// "Implements:" and "Verifies:" comment markers that tie code and tests to
// requirement refs such as GIT-SP-0003.R2. Each hit records the file, the
// line, the kind and the enclosing symbol: Go through go/parser, TS/JS and
// Python through a light line heuristic, every other file type as a whole.
//
// The scan result is derived data. It lives in memory, is rebuilt fully on
// demand or incrementally from a list of changed paths, and is never written
// into a spec: markers and the spec's own trace entries are unioned by the
// consumers (coverage, impact), neither overrides the other.
//
// On top of the scan, BuildGraph builds the requirement trace graph: markers
// unioned with each requirement's trace: entries, plus the stories and tasks
// that implement or modify it, queryable from a requirement to its code and
// tests and from a changed path, line range or symbol back to requirements.
// Engine keeps one graph current per working tree and is the tracer a native
// host installs into a vault (vault.RequirementTracer).
//
// ParseReport, TestResolver, ResultStore and MatchRequirements ingest test
// results: go test -json, JUnit XML and Vitest JSON reports are parsed,
// each test is mapped to the trace ref its markers spell, its last result
// is kept in a per-machine cache outside the repository, and the results
// of a requirement's linked tests are aggregated (GIT-US-0115).
//
// Coverage turns that evidence, the requirement's verified: stamp and the
// changes gitops reports since the evidence's commit into the computed
// coverage state — untested, failing, suspect, passing — and offers the
// stamp a passing run allows; it is the coverage backend a native host
// installs into a vault (vault.RequirementCoverage, GIT-US-0116).
//
// The package walks the filesystem and is therefore native only; it imports
// internal/core for the ref grammar, never the other way round, and neither
// internal/core nor internal/vault may import it (the WASM build stays free
// of it).
package trace
