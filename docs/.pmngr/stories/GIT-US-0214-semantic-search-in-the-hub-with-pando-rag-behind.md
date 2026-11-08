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
updated: 2026-11-08T17:59:20Z
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

## Notes

Part of the gintrack hub PoC; see ADR-043 and ADR-044.
