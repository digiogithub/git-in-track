---
id: GIT-US-0019
type: story
title: Show remote references from index snapshots
status: in_review
priority: high
parent: GIT-EP-0004
milestone: GIT-M-0004
author: team
labels: [core, server, web]
estimate: 5
created: 2026-09-03T00:00:00Z
updated: 2026-09-04T00:00:00Z
links:
  - { kind: blocked_by, target: GIT-US-0016 }
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

- [x] `gintrack snapshot` writes a deterministic `.pmngr/index/<projectKey>.json`.
- [x] Snapshots are stable across runs so they do not churn the git history.
- [x] Cards for uncloned projects render from the snapshot with all displayed fields.
- [x] Remote cards are visually distinct, read-only, and show the snapshot's age.
- [x] Remote cards link to the item's file URL on the configured host.
- [x] Attempting to edit a remote card explains how to clone the project instead.
- [x] A missing or malformed snapshot degrades to a placeholder card, never a crash.
- [x] Cloning a project later replaces its remote cards with live ones automatically.

## Notes

Implemented in Phase 3, after GIT-US-0016 and GIT-US-0017.

- `internal/core/remoteref.go` reads `.pmngr/index/<projectKey>.json`, grades its
  age against `snapshots.max_age_days` and builds the host link from `web_url`
  and `default_branch`. `BuildBoardView` renders the cards of an uncloned project
  from that set, in the column its published status maps to, with the board
  filters applied exactly as they are to a live item.
- Writes: a remote card refuses any move that would touch the project repository
  (`repo_not_cloned`), while a re-order inside one column still writes the board
  file, because the order lives in the team repository (doc 04 R-REM-1).
- Generation: `gintrack snapshot [KEY...]`, plus `snapshot.list` /
  `snapshot.refresh` on the core contract and `GET`/`POST /api/v1/snapshots`.
  A regenerated file is written only when its content changed — see
  [ADR-014](../../adr/ADR-014-snapshots-stay-on-the-main-branch.md), which
  settles doc 04's "snapshot churn" open question.
- Deferred, with the phases that own them: refreshing on `gintrack sync` and on
  the companion watcher (R-SNAP-6 a and b), and the `source` block of the file,
  which needs the git backend of Phase 4.
