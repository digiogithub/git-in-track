---
id: GIT-US-0015
type: story
title: Auto-detect the companion and upgrade the web app
status: in_progress
created: 2026-09-03T00:00:00Z
updated: 2026-09-04T05:30:00Z
started: 2026-09-04T05:06:40Z
author: team
priority: high
parent: GIT-EP-0003
milestone: GIT-M-0003
estimate: 3
labels: [web]
links:
  - kind: blocked_by
    target: GIT-US-0014
---

## Description

As a user, I want the app to use the companion when it is running and fall back to the
browser core when it is not, so that installing the binary is an upgrade rather than a
different product I have to learn.

The web app probes `GET /api/health` at startup and periodically afterwards. When the
companion appears, it swaps the core adapter from the WASM/File System Access
implementation to the REST/WebSocket one, keeping the current route and view state. When
the companion disappears, it degrades gracefully and tells the user what changed.

The active mode is always visible, and the differences between modes are explained in one
place rather than discovered by surprise.

## Acceptance Criteria

- [x] The app detects a running companion within 2 seconds of startup.
- [x] Switching adapters preserves the current route, filters and scroll position.
- [x] No page reload is required in either direction.
- [x] The current mode and its capabilities are visible in the UI.
- [x] Losing the companion mid-session degrades without data loss or an error page.
- [x] Detection never blocks first paint and fails silently when the port is closed.
- [x] Adapter swapping is covered by tests with a mocked health endpoint.
