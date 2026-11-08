---
id: GIT-US-0214
type: story
title: Semantic search in the hub with pando/rag behind SemanticSearcher
status: todo
priority: medium
parent: GIT-EP-0034
author: claude-code
labels: [server, agent-ok]
estimate: 5
created: 2026-11-08T17:59:20Z
updated: 2026-11-08T23:00:00Z
links:
  - { kind: blocked_by, target: GIT-US-0207 }
---

## Description

Install a `vault.SemanticSearcher` backed by the `pando/rag` library (one SQLite per project, pure-Go driver) and reindex per commit with a `rag.apply{project, from, to}` syncengine job using the library's watermark (ADR-043 §5). Start against a fake store with the agreed interface; switch to the real module when claude@pando publishes it.

## Acceptance Criteria

- [ ] Hits resolve back to live documents by rev; stale hits are dropped.
- [ ] Restart resumes from the stored watermark without a full reindex.
- [ ] FTS-only mode works with no embedder configured.
- [ ] No SQLite import reaches `internal/core` or `internal/vault`.
- [ ] gintrack-hub stops running the file watcher (`Watch: true` in `internal/hub/hub.go`): what a pull brings in is reindexed from the commit range, by the same job that feeds the RAG index.

## Notes

Part of the gintrack hub PoC; see ADR-043 and ADR-044.

The hub skeleton (GIT-US-0207) keeps the companion's file watcher on, so that a pull is reindexed; that is a stopgap accepted on 2026-11-08 until this story reindexes by commit. Until the `pando/rag` module is tagged, a `replace` to the local checkout is used only on this story's branch and never committed in go.mod.
