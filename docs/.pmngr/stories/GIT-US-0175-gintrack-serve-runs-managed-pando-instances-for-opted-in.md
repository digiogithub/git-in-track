---
id: GIT-US-0175
type: story
title: gintrack serve runs managed Pando instances for opted-in repos
status: done
priority: high
parent: GIT-EP-0031
assignees: [claude]
author: mcp
labels: [server, agent-ok]
created: 2026-09-29T18:53:52Z
updated: 2026-09-29T20:58:55Z
started: 2026-09-29T19:04:36Z
closed: 2026-09-29T20:58:55Z
---

## Description

Wires the supervisor into `gintrack serve` (ADR-039). Depends on the supervisor package and the configuration story of GIT-EP-0031.

## Acceptance Criteria

- [x] `serve` starts one instance per opted-in repo, capped by `search.pando.managed.maxInstances`, and registers the code project under `pando.SanitizeProjectID(root)` with the code graph enabled.
- [x] Semantic search fans out to every instance in parallel within the existing 300 ms budget; impact tiers 2 and 3 use the instance of the repo in question.
- [x] `/api/v1/search/settings` reports the managed state per repo; the KB half of a reindex restarts the instance and `kbNote` says so.
- [x] Instances stop when `serve` stops; tests use the fake `pando`.
