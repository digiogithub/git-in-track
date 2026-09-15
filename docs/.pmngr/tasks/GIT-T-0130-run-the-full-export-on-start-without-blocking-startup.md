---
id: GIT-T-0130
type: task
title: Run the full export on start without blocking startup
status: done
priority: medium
parent: GIT-US-0073
milestone: GIT-M-0013
author: mcp
labels: [server, performance]
estimate: 3
created: 2026-09-13T13:18:31Z
updated: 2026-09-15T16:44:36Z
started: 2026-09-15T15:16:23Z
closed: 2026-09-15T16:44:36Z
---

## Description

Walk every project's items and KB pages once at startup through the vault, serialise each document and write only those whose content hash or mtime differs from what is already on disk, then prune corpus files with no surviving source. Run it in a goroutine started from `Server.Start` so it never delays the listener, with progress published on the hub in the shape `sync.progress` uses (`internal/server/sync.go:239`). It must be cancellable by the server's shutdown context.

## Acceptance Criteria

- [ ] The server accepts requests immediately while the first export runs in the background.
- [ ] A second export over an unchanged corpus writes nothing.
- [ ] Corpus files whose source is gone are pruned.
- [ ] Shutdown cancels an in-flight export cleanly, and `go test -race` covers all four behaviours including a 10 000-item timing check.
