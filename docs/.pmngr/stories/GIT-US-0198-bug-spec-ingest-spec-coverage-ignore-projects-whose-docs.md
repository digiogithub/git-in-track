---
id: GIT-US-0198
type: story
title: "Bug: `spec ingest`/`spec coverage` ignore projects whose docs folder is dot-prefixed (e.g. `.kb/`) — requirements stay \"untested\" despite passing results"
status: done
priority: high
assignees: [claude-code]
created: 2026-09-30T21:09:29Z
updated: 2026-10-01T07:45:30Z
started: 2026-09-30T21:42:29Z
closed: 2026-10-01T07:45:30Z
links:
  - { kind: duplicated_by, target: GIT-T-0239 }
deleted: true
---

## Summary
In a repository whose backlog lives in a dot-prefixed docs folder (registered with `docs: .kb`, project at `.kb/.pmngr/project.yaml`), `gintrack spec ingest` records test results in the cache but never maps them to requirements, never writes `<docs>/.pmngr/verify.json`, and `gintrack spec coverage` / MCP `spec_coverage` / `verify_requirement` report every requirement as `untested` (`no-results`, every linked test `missing`).

## Reproduction (repo PANDO, `/www/MCP/Pando/pando`, jj colocated)
1. `gintrack ls` → `pando  project  jj (colocated)  /www/MCP/Pando/pando  .kb  PANDO`.
2. Specs PANDO-SP-0001..0004 (22 requirements) with `trace.tests` such as `internal/config/model_auto_mode_test.go#TestModelAutoModeRouteValidation`.
3. `go test -json ./internal/config/ ... > go.json` (all pass) and `gintrack spec ingest go.json` →
   `go.json (go): 1463 tests — 1462 passed, 0 failed, 1 skipped, 0 unmapped` and `cache …/test-results/53b0f21c96b7b525.json: 1463 added` — **but no requirement lines are printed**.
4. The cache does contain the entries (`path: internal/config/model_auto_mode_test.go`, `symbol: TestModelAutoModeRouteValidation`, `result: pass`).
5. `.kb/.pmngr/verify.json` is not created.
6. `gintrack spec coverage --project PANDO` → all 22 rows `untested 0/N no-results`; `gintrack spec verify PANDO-SP-0001.R2` → `unstamped … no-results`. Same through the MCP tools. Re-running `gintrack index` and `--commit <HEAD>` changes nothing.

## Root cause (analysis)
- `cmd/gintrack/spec.go` `runSpecIngest` builds the trace with `trace.RepositoryTrace(ctx, root)`.
- `internal/trace/verify.go:193` `RepositoryTrace` calls `core.DiscoverProjects(fsys, ".")` → `DiscoverProjectsWith(fs, DiscoveryOptions{Roots: ["."]})` with **no `DocsFolders`**.
- The shallow rule probes the root and one level below, but `internal/core/index.go:222` `skipDirName` skips every name starting with `.` → `.kb` is never probed. `projects` is empty and `RepositoryTrace` returns `g == nil`, so `MatchRequirements` / `VerificationEntries` / `RecordVerification` are all skipped silently.
- The registered repo config declares the docs folder (`docs: .kb`, see `internal/config/repo.go:195` and `cmd/gintrack/workspace.go:89-104`, which do pass `DocsFolders`), but that information is not threaded into `RepositoryTrace`.
- `spec coverage` (`coverage.list`) therefore finds no evidence for those requirements. Worth checking whether its result-evidence lookup has the same discovery gap.

## Expected
Ingest resolves the project through the same discovery as the index (declared docs folders of the registered repository, or at least the repo's `docs` path), prints the per-requirement aggregate, writes `.kb/.pmngr/verify.json`, and coverage/verify show `passing`.

## Suggested fix
- Pass the registered repository's declared docs folders into `RepositoryTrace` (e.g. `RepositoryTrace(ctx, root, docsFolders []string)` using `DiscoverProjectsWith`), resolved from the config by root in `runSpecIngest` / `spec verify` / the coverage handler.
- When the trace has no project, fail loudly or warn (`no project backlog found under <root>; declared docs folder?`) instead of succeeding silently.
- Add a regression test with a repo whose backlog is under `.kb/.pmngr`.

## Environment
gintrack from `/home/sevir/bin/gintrack`, source `/www/git-in-track`; date 2026-09-30.
