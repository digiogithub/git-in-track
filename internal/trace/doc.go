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
// The package walks the filesystem and is therefore native only; it imports
// internal/core for the ref grammar, never the other way round, and neither
// internal/core nor internal/vault may import it (the WASM build stays free
// of it).
package trace
