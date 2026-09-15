---
id: GIT-T-0134
type: task
title: Subscribe to hub events for incremental export
status: done
priority: medium
parent: GIT-US-0073
milestone: GIT-M-0013
author: mcp
labels: [server]
estimate: 3
created: 2026-09-13T13:18:36Z
updated: 2026-09-15T16:44:37Z
started: 2026-09-15T15:16:24Z
closed: 2026-09-15T16:44:37Z
---

## Description

Subscribe the exporter to the hub (`internal/server/hub.go:72-104`) on `item.changed` and `file.changed`, coalescing events per target path over a short debounce window so a burst of watcher events becomes one write. An `item.changed` event rewrites that item's document or unlinks it when the item is gone; a `file.changed` event with `IsKb` rewrites the corresponding KB page. Failures are logged and retried on the next event rather than killing the subscriber goroutine.

## Acceptance Criteria

- [ ] A single item change rewrites exactly one corpus file.
- [ ] A burst of events for the same path produces one write.
- [ ] A deleted item or page removes its corpus file.
- [ ] A write failure does not stop the subscriber, and `go test -race` covers all four cases.
