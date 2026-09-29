---
id: GIT-US-0182
type: story
title: Stop tier 3 timing out right after a working-tree edit in managed mode
status: backlog
priority: low
author: mcp
labels: [server, agent-ok]
created: 2026-09-29T22:56:37Z
updated: 2026-09-29T22:56:37Z
---

## Description

In managed mode (ADR-039), tier 3 sometimes says `unavailable (Pando did not answer in time)` right after a working-tree edit, and a retry succeeds (#107, §10.6). The generated `.pando.toml` sets `KBWatch = true`, so Pando is probably busy re-indexing when the query arrives.

## Acceptance Criteria

- [ ] The cause is confirmed (KB watch reindex or something else).
- [ ] Tier 3 either waits briefly or retries once within its budget, or managed config tunes the watcher, so a query right after an edit answers.
- [ ] A test with a fake slow-then-ready instance covers it.
