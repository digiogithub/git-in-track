---
id: GIT-T-0128
type: task
title: Implement the atomic corpus writer with path guarding
status: todo
priority: medium
parent: GIT-US-0073
milestone: GIT-M-0013
author: mcp
labels: [server, security]
estimate: 3
created: 2026-09-13T13:18:25Z
updated: 2026-09-13T13:18:25Z
---

## Description

Add the writer half of `internal/server/pandosync.go`: resolve the corpus root from `config.Config.CacheDir(configPath)` plus `pando-kb/<project>/`, create directories as needed, and write each document to a temp file in the same directory before renaming it into place, so Pando's fsnotify watcher never reads a partial file. Every target path is derived from a validated item id or a cleaned vault-relative page path and verified to stay inside the corpus root, following `internal/mcp/paths.go`. Deleting a document unlinks it and prunes now-empty directories.

## Acceptance Criteria

- [ ] Writes are atomic; a concurrent reader sees either the old or the new content.
- [ ] A page path containing traversal segments is rejected and nothing is written outside the root.
- [ ] Deletion removes the file and any directory it left empty.
- [ ] `go test -race ./internal/server/...` covers atomic write, traversal rejection and deletion.
