---
id: GIT-T-0163
type: task
title: Implement the Pando-backed searcher with id resolution
status: todo
priority: medium
parent: GIT-US-0082
milestone: GIT-M-0013
author: mcp
labels: [server]
estimate: 5
created: 2026-09-13T13:19:15Z
updated: 2026-09-13T13:19:15Z
---

## Description

Add a native searcher in `internal/server` that satisfies `core.Searcher` by calling `internal/pando.Client.SearchKB` and mapping each returned `file_path` back to an item id or a KB page path using the corpus layout the exporter writes. Hits that no longer resolve to a live item or page are dropped rather than returned as dangling results. Each hit carries its score and the matched chunk as a snippet. Keep this entirely in native code; nothing here may move into `internal/core`.

## Acceptance Criteria

- [ ] Corpus paths resolve back to item ids and KB page paths for both document kinds.
- [ ] Unresolvable hits are dropped and counted for logging, not returned.
- [ ] Each hit carries a score, a snippet and the semantic origin.
- [ ] `go test -race ./internal/server/...` covers resolution, dropping and the empty-result case.
