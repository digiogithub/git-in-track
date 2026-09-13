---
id: GIT-T-0158
type: task
title: Write the remote comment id back and render the attribution line
status: todo
priority: medium
parent: GIT-US-0068
milestone: GIT-M-0011
author: mcp
labels: [core, server, agent-ok]
estimate: 3
created: 2026-09-13T13:19:08Z
updated: 2026-09-13T13:19:08Z
---

## Description

On a successful create, write the returned YouTrack comment id and url into the comment's `external` block through the vault, quoting the comment's rev so a concurrent edit is not clobbered. Add the attribution renderer: a small template with the author and the gintrack item id, configurable per project, defaulting to a single trailing line. The item id must not be wrapped in a link since YouTrack auto-links bare ids.

## Acceptance Criteria

- [ ] The remote comment id and url are written back rev-guarded; a stale rev is reported, not forced.
- [ ] The attribution line renders from a project-configurable template with author and item id.
- [ ] The item id is emitted bare so YouTrack's auto-linking is not double-wrapped.
- [ ] `go test -race ./internal/server/...` covers the write-back, the stale-rev path and template rendering.
