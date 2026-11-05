---
id: GIT-US-0204
type: story
title: "Settings toggle to hide the team surface: team panel, boards, retros and metrics"
status: in_review
priority: medium
parent: GIT-EP-0033
author: mcp
assignees: [claude-code]
labels: [web, agent-ok]
estimate: 2
created: 2026-11-05T14:58:06Z
updated: 2026-11-05T15:04:33Z
---

## Description

Some users connect a team repository but do not want the team surface in their workspace. Add a per-browser preference in Settings, "Show team views", on by default.

## Acceptance Criteria

- [ ] Settings page has a "Show team views" switch, persisted in `localStorage` through `web/src/app/ui-prefs.ts`; default on (current behaviour).
- [ ] When off: the workspace team panel / team selector is hidden, and the sidebar/navigation entries for Boards, Sprints, Retros and Metrics are hidden, even when a team repository is connected.
- [ ] When off, navigating directly to `/boards`, `/retros`, `/metrics` (and their children) shows a short notice with a link to Settings to turn team views back on, instead of the team page.
- [ ] Nothing in the repository changes: the team repository stays connected and is untouched.
- [ ] Vitest coverage for the preference, hidden navigation and the guarded routes.
- [ ] `docs/05-web-app.md` documents the setting.
