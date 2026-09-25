---
id: GIT-US-0149
type: story
title: List branches and recent commits for the impact view pickers
status: done
priority: medium
parent: GIT-EP-0027
milestone: GIT-M-0015
author: mcp
labels: [server, web, git, agent-ok]
estimate: 3
created: 2026-09-24T21:55:53Z
updated: 2026-09-25T11:15:00Z
closed: 2026-09-25T11:15:00Z
---

## Description

The impact view (GIT-US-0131, PR #60) has free-text base and head fields, because no companion route lists refs. Its first acceptance criterion, pickers for branches and recent commits, is still open.

## Acceptance Criteria

- [x] A read-only route that lists local and remote branches and the last N commits (sha, subject, date) for the git, system-git and jj backends. It follows the existing route patterns, with no change to auth or path validation.
- [x] Web provider method(s); browser-only mode answers `unavailable`.
- [x] The impact view's base and head fields become pickers backed by it, and free text is still accepted.
- [x] Go route tests and Vitest tests. docs/07 and docs/05 are updated, and GIT-US-0131's first criterion is ticked.
