---
id: GIT-US-0019
type: story
title: Show remote references from index snapshots
status: backlog
created: 2026-09-03T00:00:00Z
updated: 2026-09-03T00:00:00Z
author: team
priority: high
parent: GIT-EP-0004
milestone: GIT-M-0004
estimate: 5
labels: [core, server, web]
links:
  - kind: blocked_by
    target: GIT-US-0016
---

## Description

As a team member who has not cloned every project, I want the team board to still show me
those projects' cards, so that the board reflects the whole team's work rather than only
the repositories on my laptop.

Each project repository publishes an index snapshot — id, title, status, priority,
assignees, labels, milestone — committed to the team repository as
`.pmngr/index/<projectKey>.json`. Cards for projects that are not cloned render from that
snapshot, clearly marked read-only and stale-dated, with a link to the file on the remote
host.

`gintrack snapshot` generates and refreshes snapshots, and is designed to be run in the
project repository's own CI.

## Acceptance Criteria

- [ ] `gintrack snapshot` writes a deterministic `.pmngr/index/<projectKey>.json`.
- [ ] Snapshots are stable across runs so they do not churn the git history.
- [ ] Cards for uncloned projects render from the snapshot with all displayed fields.
- [ ] Remote cards are visually distinct, read-only, and show the snapshot's age.
- [ ] Remote cards link to the item's file URL on the configured host.
- [ ] Attempting to edit a remote card explains how to clone the project instead.
- [ ] A missing or malformed snapshot degrades to a placeholder card, never a crash.
- [ ] Cloning a project later replaces its remote cards with live ones automatically.
