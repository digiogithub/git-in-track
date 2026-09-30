---
id: GIT-US-0190
type: story
title: Make the tier-2 graph probe complete on slow Pando instances
status: done
priority: medium
assignees: [claude]
author: mcp
labels: [server, core, agent-ok]
created: 2026-09-30T15:23:15Z
updated: 2026-09-30T16:50:40Z
started: 2026-09-30T16:22:50Z
closed: 2026-09-30T16:50:40Z
---

## Description

GIT-US-0179 (#111) and GIT-US-0188 (#118) persist the tier-2 code-graph probe answer, but on the benchmark repo no probe ever completes, so tier 2 stays "still being checked":
- Pando `code_related_files` takes about 184 s per call.
- A CLI `spec impact` run exits after about 10 s. It leaves its probe running inside Pando: about 60 abandoned calls piled up during one replay, and later probes queue behind them.
- `serve`'s warm-up (`warmGraph` → `sampleSourceFiles(root, 5)` in internal/server/graph_store.go) probes the first 5 non-test files in path order (`cmd/gintrack/{add,agent,agent_merge,completion,config}.go`), probably files with no coupling. It stores only a found graph, so after 45 min nothing was stored.

## Acceptance Criteria

- [x] The warm-up samples files likely to have call edges (for example spread across packages, preferring non-main packages with many symbols) and stores a result with a short TTL even when no edges are found.
- [x] Short-lived CLI runs do not start a new probe when one is already running for that instance (a lock or marker in the store), so abandoned calls do not pile up in Pando.
- [x] With a managed Pando on the benchmark repo, tier 2 answers `ok` from the store within minutes of `serve` starting (replay S1/S3/S4).
- [ ] Consider an upstream Pando change (a cheap "has call edges" or edge count in `code_get_project_stats`) and record the finding in docs/21; if it is cheap, file it upstream.

## Notes

Done in PR #120. The real root cause was the Pando client's 20 s per-call deadline, which meant the 184 s probe could never finish; the probe now passes its own timeout. The warm-up now waits for indexing to complete, samples AST-ranked files across packages, and stores "no edges" for 5 min. An `O_EXCL` in-flight marker gives one probe per instance across processes, and exiting CLI runs cancel their call, which Pando honours.

Replay: the store was populated 286 s after `serve` started, and S1/S3/S4 give tier 2 `ok` in about 0.4 s.

Pando finding, recorded in docs/21 §6.1 but not yet filed upstream: `code_edges` has no index on `src_file`. Adding `(project_id, src_file, edge_type)` cut the query from 52.5 s to 0.146 s. A second index for in-calls and edge counts in `GetProjectStats` are also proposed.
