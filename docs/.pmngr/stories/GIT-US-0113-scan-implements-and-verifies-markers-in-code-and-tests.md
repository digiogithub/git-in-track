---
id: GIT-US-0113
type: story
title: Scan Implements and Verifies markers in code and tests
status: done
priority: high
parent: GIT-EP-0024
milestone: GIT-M-0015
author: claude
labels: [server, cli, agent-ok]
estimate: 5
created: 2026-09-24T12:10:07Z
updated: 2026-09-25T11:15:00Z
closed: 2026-09-25T11:15:00Z
links:
  - { kind: blocked_by, target: GIT-US-0105 }
---

## Description

As the trace engine, I want every `// Implements: GIT-SP-NNNN.R<n>` and `// Verifies: GIT-SP-NNNN.R<n>` marker found with the file and enclosing symbol, so traces travel with the code through refactors.

## Acceptance Criteria

- [x] A native package (e.g. `internal/trace`) walks the repository (honouring `.gitignore`, skipping `web/dist`, `node_modules`, build output) and extracts markers in `//`, `#` and `/* */` comments, one or several refs per marker.
- [x] Each hit records path, line, kind (implements/verifies) and the enclosing symbol (Go via `go/parser`; TS/JS via a light heuristic for functions, `describe`/`it`/`test`).
- [x] Results are a derived cache, rebuilt incrementally from `ChangedFiles` or fully on demand, never a source of truth.
- [x] Markers pointing to a missing spec or block are reported as dangling.
- [x] Table-driven tests with fixture files; the package is not imported by `internal/core` or `internal/vault`; `make wasm` passes.

## Notes

Decision 2 of GIT-T-0238.

`Cache.Update(ctx, paths)` takes the repository-relative paths of a diff; `gitops.ChangedFiles` (GIT-US-0112) is not in this change, so the caller feeds it the paths of each `FileChange` (both sides of a rename). The cache is in memory only; persisting it next to `index.json` would need the R-LOC-5 `.gitignore` snippet and is left to the consumer stories.
