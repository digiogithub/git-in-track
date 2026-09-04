---
id: GIT-EP-0003
type: epic
title: Companion CLI
status: done
created: 2026-09-03T00:00:00Z
updated: 2026-09-04T05:48:51Z
closed: 2026-09-04T05:48:51Z
started: 2026-09-04T05:06:40Z
author: team
priority: critical
milestone: GIT-M-0003
estimate: 24
labels: [cli, server, web]
links:
  - kind: blocked_by
    target: GIT-EP-0002
---

## Description

Phase 2. Remove the browser's limits for users who install the binary. `gintrack serve`
binds `127.0.0.1:7317`, serves the embedded web app through `go:embed`, watches the file
system with fsnotify, indexes natively at full speed, and exposes a REST plus WebSocket
API. The web app detects the companion and upgrades to it transparently.

The same `internal/core` code powers both modes, so behaviour cannot drift; the only
difference is speed, file watching, and the availability of native git.

## Acceptance Criteria

- [ ] Editing a file in an external editor updates the open UI in under 300 ms.
- [ ] A 50,000-file vault indexes natively in under 5 seconds.
- [ ] Every Phase 1 flow behaves identically in both modes, proven by one shared E2E suite
      run twice.
- [ ] The server refuses a non-loopback bind without `--allow-remote` and rejects requests
      with a foreign `Origin`.
- [ ] The watcher is correct on Linux, macOS and Windows, with a polling fallback.

## Notes

Stories: GIT-US-0012 … GIT-US-0015. See docs/11-roadmap.md, milestone 3.
Primary risk: R6 (Windows file watching).
