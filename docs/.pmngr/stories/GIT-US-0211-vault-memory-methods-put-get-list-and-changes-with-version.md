---
id: GIT-US-0211
type: story
title: "Vault memory methods: put, get, list and changes with version and dedup"
status: done
priority: high
parent: GIT-EP-0034
author: claude-code
assignees: [claude-code]
labels: [core, agent-ok]
estimate: 5
created: 2026-11-08T17:59:20Z
updated: 2026-11-09T13:00:00Z
links:
  - { kind: blocked_by, target: GIT-US-0210 }
---

## Description

Add `memory.put` (batch), `memory.get`, `memory.list` and `memory.changes{since, scopes}` to `internal/vault`, independent of any wire protocol: upsert by `key`, server-side `content_hash` recomputation and dedup, monotonic per-project `version` in front matter, forget as `forgotten` with `valid_to`, tombstones in the changes feed, one commit per project per batch, `rev`/`base_version` optimistic locking.

## Acceptance Criteria

- [x] Upserting the same content twice reports a duplicate and writes nothing.
- [x] `version` is strictly increasing per project and survives an index rebuild.
- [x] `memory.changes` with a cursor returns only newer records and tombstones, in version order.
- [x] A stale `rev` or `base_version` fails with `stale_revision`.
- [x] Pure Go: compiles under `GOOS=js GOARCH=wasm`.

## Notes

Part of the gintrack hub PoC; see ADR-043 and ADR-044.
