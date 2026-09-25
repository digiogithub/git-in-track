---
id: GIT-US-0113
type: story
title: Scan Implements and Verifies markers in code and tests
status: backlog
priority: high
parent: GIT-EP-0024
milestone: GIT-M-0015
author: claude
labels: [server, cli, agent-ok]
estimate: 5
created: 2026-09-24T12:10:07Z
updated: 2026-09-24T12:10:07Z
links:
  - { kind: blocked_by, target: GIT-US-0105 }
---

## Description

As the trace engine, I want every `// Implements: GIT-SP-NNNN.R<n>` and `// Verifies: GIT-SP-NNNN.R<n>` marker found with the file and enclosing symbol, so traces travel with the code through refactors.

## Acceptance Criteria

- [ ] A native package (e.g. `internal/trace`) walks the repository (honouring `.gitignore`, skipping `web/dist`, `node_modules`, build output) and extracts markers in `//`, `#` and `/* */` comments, one or several refs per marker.
- [ ] Each hit records path, line, kind (implements/verifies) and the enclosing symbol (Go via `go/parser`; TS/JS via a light heuristic for functions, `describe`/`it`/`test`).
- [ ] Results are a derived cache, rebuilt incrementally from `ChangedFiles` or fully on demand, never a source of truth.
- [ ] Markers pointing to a missing spec or block are reported as dangling.
- [ ] Table-driven tests with fixture files; the package is not imported by `internal/core` or `internal/vault`; `make wasm` passes.

## Notes

Decision 2 of GIT-T-0238.
