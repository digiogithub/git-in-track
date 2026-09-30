---
id: GIT-US-0188
type: story
title: Persist the tier-2 graph probe for external Pando and the spec impact CLI
status: done
priority: low
assignees: [claude]
author: mcp
labels: [core, server, cli, agent-ok]
created: 2026-09-30T12:35:09Z
updated: 2026-09-30T15:23:23Z
started: 2026-09-30T13:07:21Z
closed: 2026-09-30T15:23:23Z
---

## Description

GIT-US-0179 (#111) runs the tier-2 code-graph probe in the background and persists its answer for managed instances (`<cacheDir>/pando/<instance>/graph-probe.json`, keyed by pid and state change). `gintrack serve` warms it once the code project is registered. Two gaps remain:
- an external (non-managed) Pando has no persistence, because there is no instance generation to key the answer by;
- `gintrack spec impact` in the CLI does not wire the graph store.

So a short-lived CLI run, including CI and the git hook, on a slow Pando still answers "still being checked". The S1/S3/S4 replay recheck of GIT-US-0179 AC3 was not done either.

## Acceptance Criteria

- [x] `gintrack spec impact` reuses a persisted probe answer (managed: the instance's store; external: a TTL-keyed file under the cache dir keyed by endpoint + project id).
- [x] Tests cover a CLI run hitting a stored answer and an expired one.
- [ ] The benchmark S1/S3/S4 runs are replayed with a managed Pando and tier 2 is no longer `unavailable` on them (record in the benchmark doc).

## Notes

Done in PR #118. The managed store was already reachable from the CLI. External Pando now gets a TTL-only store at `<cacheDir>/pando/external/graph-probe-<hash>.json`, keyed by a hash of the endpoint and project; the token is not part of the key.

Replay (benchmark §10.8): with a seeded store, S1/S3/S4 give tier 2 `ok` in about 0.4 s. Without one, no probe ever completes, because `code_related_files` takes about 184 s and the warm-up samples files with no coupling. That gap moves to GIT-US-0190.
