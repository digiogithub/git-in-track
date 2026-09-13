---
id: GIT-T-0117
type: task
title: Write the journal atomically with coalesced writes
status: todo
priority: medium
parent: GIT-US-0070
milestone: GIT-M-0011
author: mcp
labels: [server, agent-ok]
estimate: 3
created: 2026-09-13T13:18:11Z
updated: 2026-09-13T13:18:11Z
---

## Description

Add a journal writer persisting the queue to `jobs.json` under `config.CacheDir` (`internal/config/path.go:96`), written to a temporary file and renamed, with restrictive permissions. Coalesce writes behind a short timer so a burst of state transitions produces one write, and never block a worker on disk I/O. Redact credentials from payloads and error strings before they are written.

## Acceptance Criteria

- [ ] The journal is written atomically and never left half-written, proved by a test that interrupts between write and rename.
- [ ] A burst of transitions produces a bounded number of writes.
- [ ] No credential appears in the journal, asserted by a test.
- [ ] `go test -race ./internal/syncengine/...` covers the writer over a temporary directory.
