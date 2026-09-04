---
id: GIT-M-0003
type: milestone
title: Phase 2 — Companion CLI
status: done
created: 2026-09-03T00:00:00Z
updated: 2026-09-04T05:48:51Z
closed: 2026-09-04T05:48:51Z
started: 2026-09-04T05:06:40Z
author: team
labels: [cli, server]
links:
  - kind: relates_to
    target: GIT-EP-0003
custom:
  phase: 2
  version: v0.3.0
---

## Description

Phase 2 of the roadmap. The gintrack binary serves the embedded web app on
127.0.0.1:7317, watches the file system, indexes natively and exposes a REST plus
WebSocket API. The web app auto-detects it and upgrades without a reload.

## Acceptance Criteria

- [ ] External file edits reach the open UI in under 300 ms.
- [ ] 50,000-file vault indexes natively in under 5 s.
- [ ] One shared E2E suite passes in both operating modes.
- [ ] Non-loopback bind requires `--allow-remote`; foreign origins rejected.
- [ ] Watcher correct on Linux, macOS and Windows, with a polling fallback.

## Notes

Epic: GIT-EP-0003. Stories: GIT-US-0012 … GIT-US-0015. Risk: R6.
