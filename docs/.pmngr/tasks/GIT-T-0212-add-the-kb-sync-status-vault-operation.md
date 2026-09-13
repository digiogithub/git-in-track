---
id: GIT-T-0212
type: task
title: Add the KB sync status vault operation
status: todo
priority: medium
parent: GIT-US-0090
milestone: GIT-M-0011
author: mcp
labels: [core, server, agent-ok]
estimate: 3
created: 2026-09-13T13:21:06Z
updated: 2026-09-13T13:21:06Z
---

## Description

Add `youtrack.kb.status` to the vault dispatch table taking `{project, path, recursive}` and returning per page `{path, linked, articleId, url, state, syncedAt}` with `state` one of `unlinked`, `in_sync`, `local_ahead`, `remote_ahead`, `conflict`. It reads the remote only when explicitly asked, so a tree view does not fan out into hundreds of requests; otherwise it answers from `external.synced_at` and the local rev alone.

## Acceptance Criteria

- [ ] The method exists in the dispatch table and returns the five states per page.
- [ ] Without the remote-check flag it makes no network request.
- [ ] A recursive call answers for a whole subtree in one response.
- [ ] `go test -race ./internal/vault/...` covers every state and the no-network default.
