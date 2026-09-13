---
id: GIT-US-0070
type: story
title: JSON job journal in the cache directory with replay and pruning
status: backlog
priority: medium
parent: GIT-EP-0015
milestone: GIT-M-0011
author: mcp
labels: [server, docs]
estimate: 5
created: 2026-09-13T13:13:25Z
updated: 2026-09-13T13:13:25Z
---

## Description

As a user whose companion restarted mid-import, I want queued and failed jobs to still be there afterwards, so that a crash or a laptop lid does not silently lose half a sync and leave the backlog in an unknown state.

Persist the queue to a JSON journal at `jobs.json` under `config.CacheDir` (`internal/config/path.go:96`), written atomically with the tmp-plus-rename discipline the core already uses for items (`writeFileAtomic`, `internal/core/store.go:1051`). The journal records every job with its kind, coalescing key, payload, state, attempts and last error; it is appended or rewritten on state transitions, coalesced so a busy queue does not fsync per job. On `Engine` start the journal is read back: `queued` jobs return to the queue, jobs that were `running` when the process died are re-queued as `queued` with their attempt count intact (handlers must be idempotent, which the engine documents as a contract), and `done` entries older than the retention window are pruned. A corrupt or unreadable journal is never fatal — it is renamed aside and the engine starts empty with a warning, because the journal is derived state and the Markdown files remain the source of truth.

This is the one place the engine touches the filesystem, and the "no state outside Markdown and YAML" rule in AGENTS.md permits it precisely because it is a rebuildable cache, never authoritative.

## Acceptance Criteria

- [ ] The engine writes `jobs.json` under `config.CacheDir`, atomically (tmp file plus rename) and with restrictive permissions.
- [ ] Queued and failed jobs survive a restart; `done` jobs are pruned after a configurable retention window, default 7 days.
- [ ] Jobs recorded as `running` at start-up are re-queued with their attempt count preserved, and the idempotence contract for handlers is documented in the package doc comment.
- [ ] A corrupt, truncated or unreadable journal is moved aside and logged, and the engine starts with an empty queue rather than failing to start.
- [ ] Journal writes are coalesced so a burst of state transitions does not produce one write per transition.
- [ ] The journal never contains a credential, and payloads are checked for that in a test.
- [ ] `docs/07-cli-and-api.md` documents the journal location, its format and its retention, and states that it is derived data safe to delete.
- [ ] `go test -race ./internal/syncengine/...` covers write, replay, prune and the corrupt-file path over a temporary directory.

## Notes

`config.CacheDir` (`internal/config/path.go:96`) and `StateDir` (`:92`) are the existing locations for derived data; the on-disk index cache already lives there and is the precedent for "cache that can always be rebuilt".

Do NOT introduce an embedded database — explicitly ruled out for this phase and an ADR-level decision. Do NOT record sync state inside the repository files as part of this story: per-item `external.synced_at` is owned by GIT-EP-0011's `external` field and by the importer, not by the journal.
