---
id: GIT-US-0013
type: story
title: Watch the file system and stream changes over WebSocket
status: backlog
created: 2026-09-03T00:00:00Z
updated: 2026-09-03T00:00:00Z
author: team
priority: critical
parent: GIT-EP-0003
milestone: GIT-M-0003
estimate: 8
labels: [server, performance]
links:
  - kind: blocked_by
    target: GIT-US-0012
---

## Description

As a user who edits files in my own editor, I want the open UI to update by itself, so that
git-in-track never shows me a stale version of my own repository.

`internal/watcher` wraps fsnotify with recursive watching, ignore rules (`.git`,
`node_modules`, build output), and debounced event coalescing. Because watchers drop events
under load — especially on Windows and network drives (risk R6) — the watcher verifies
rather than trusts: after each event burst it re-stats the affected subtree and compares
content hashes, and a periodic reconciliation sweep catches anything missed.

Changes are pushed to connected clients over a WebSocket channel as typed events, and the
web app applies them to the query cache without a reload.

## Acceptance Criteria

- [ ] An external edit reaches the open UI in under 300 ms.
- [ ] Create, modify, delete, rename and directory moves are all handled.
- [ ] Events are debounced and coalesced; a bulk `git checkout` produces one refresh.
- [ ] A dropped event is recovered by hash verification within one sweep interval.
- [ ] `--watch-mode=events|poll|hybrid` works; `hybrid` is the default on Windows and on
      detected network or synced folders.
- [ ] The watcher stops cleanly on context cancellation and leaks no goroutines.
- [ ] Integration tests run on Linux, macOS and Windows in CI.
- [ ] The WebSocket reconnects with backoff and resynchronises on reconnect.
