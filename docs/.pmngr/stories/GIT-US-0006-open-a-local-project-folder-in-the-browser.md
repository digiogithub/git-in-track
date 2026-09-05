---
id: GIT-US-0006
type: story
title: Open a local project folder in the browser
status: done
priority: critical
parent: GIT-EP-0002
milestone: GIT-M-0002
author: team
labels: [web]
estimate: 5
created: 2026-09-03T00:00:00Z
updated: 2026-09-04T02:37:45Z
started: 2026-09-03T21:47:06Z
closed: 2026-09-04T02:37:45Z
links:
  - { kind: blocked_by, target: GIT-US-0005 }
---

## Description

As a user with no software installed, I want to point my browser at a folder on my disk and
have git-in-track work with it, so that I can try the product without a download and
without uploading my repository anywhere.

The app uses the File System Access API (`showDirectoryPicker`) to obtain a read/write
directory handle, stores the handle in IndexedDB so the project reappears on the next
visit, and re-requests permission when the browser has expired the grant. It detects the
docs folder and `.pmngr/` inside it, reads `project.yaml`, and shows a clear error when the
chosen folder is not a git-in-track project.

Recently opened projects are listed on the start screen. Nothing ever leaves the machine.

## Acceptance Criteria

- [x] `showDirectoryPicker` obtains a read/write handle in a Chromium browser.
- [x] The handle is persisted in IndexedDB and restored on the next visit.
- [x] Expired permission triggers a single, clearly explained re-prompt.
- [x] `project.yaml` is read and its key, name and workflow drive the UI.
- [ ] A folder without `.pmngr/` offers to initialise one instead of failing.
- [x] A recent-projects list allows reopening with one click.
- [x] The UI states explicitly that files never leave the device.
- [x] Unsupported browsers are detected before the picker is offered (see GIT-US-0011).
