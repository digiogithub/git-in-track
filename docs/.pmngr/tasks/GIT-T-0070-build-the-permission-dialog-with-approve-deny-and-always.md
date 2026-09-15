---
id: GIT-T-0070
type: task
title: Build the permission dialog with approve, deny and always
status: done
priority: medium
parent: GIT-US-0061
milestone: GIT-M-0013
author: mcp
labels: [web, security]
estimate: 3
created: 2026-09-13T13:17:03Z
updated: 2026-09-15T16:43:52Z
started: 2026-09-15T16:13:33Z
closed: 2026-09-15T16:43:52Z
---

## Description

Add `PermissionDialog.tsx` over `web/src/components/ui/dialog.tsx`, showing the tool name and its arguments as escaped text with approve, deny and approve-always actions. Escape and backdrop dismissal both send an explicit denial so the run never stays parked. Approve-always is remembered per tool name in the store for the active thread only, in memory, and is forgotten on reload — a standing cross-session grant is a security decision that would need its own ADR.

## Acceptance Criteria

- [ ] The dialog shows the tool name and arguments as text and offers the three actions.
- [ ] Escape and backdrop dismissal send a denial.
- [ ] Approve-always suppresses the dialog for later calls of the same tool in the same thread and is gone after reload.
- [ ] Vitest covers all four outcomes and the always-scope.
