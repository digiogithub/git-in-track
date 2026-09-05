---
id: GIT-US-0011
type: story
title: Provide a read-only fallback for non-Chromium browsers
status: done
priority: medium
parent: GIT-EP-0002
milestone: GIT-M-0002
author: team
labels: [web, good-first-issue]
estimate: 3
created: 2026-09-03T00:00:00Z
updated: 2026-09-04T02:37:45Z
started: 2026-09-03T21:47:06Z
closed: 2026-09-04T02:37:45Z
links:
  - { kind: blocked_by, target: GIT-US-0008 }
---

## Description

As a Firefox or Safari user, I want to at least read a project's knowledge base and backlog
in my browser, so that a missing browser API does not leave me with a blank page and no
explanation.

Where the File System Access API is unavailable, the app falls back to
`<input webkitdirectory>`, reads the selected folder into memory, indexes it with the same
WASM core, and renders everything read-only. Every write affordance is hidden or disabled
with a tooltip, and a single unobtrusive banner explains the limitation and links to the
companion binary, which removes it.

This is the honest answer to risk R1: no pretending, no nagging.

## Acceptance Criteria

- [x] Capability detection runs before any picker is offered; no error is thrown.
- [x] `<input webkitdirectory>` loads a folder and the vault indexes normally.
- [ ] Every create, edit and delete affordance is hidden or disabled with an explanation.
- [x] One banner explains the limitation and links to the companion download.
- [x] The banner is dismissible and stays dismissed for the session.
- [ ] Knowledge-base rendering, browsing, filtering and search all work read-only.
- [x] The fallback is covered by a test that stubs the API as unavailable.
