---
id: GIT-US-0037
type: story
title: Manage a team's projects from settings
status: in_review
priority: high
parent: GIT-EP-0009
milestone: GIT-M-0009
author: team
labels: [core, web, server]
estimate: 5
created: 2026-09-05T00:00:00Z
updated: 2026-09-05T00:00:00Z
---

## Description

As a person who has just mounted a team repository and a project clone, we want to connect
the two from the app, so that boards and sprints have something to pull cards from without
hand-editing `team.yaml`.

The `projects:` list of `team.yaml` is the routing table of the whole product: it decides
which project a board may show, whether a card renders live from a clone or read-only from a
committed index snapshot, and where a remote blob link points (doc 04 §3.3, §7). Until this
story it was **read-only** in code. `core.LoadTeamConfig` parsed it and `TeamConfig.Project`
looked an entry up; nothing anywhere added or removed one. GIT-US-0034 brought the first
`team.yaml` writer, `core.MarshalTeamConfig`, and it writes `projects: []`. So the product
creates a team repository it then cannot connect anything to, and the only way forward is a
text editor.

This story adds the write half. `core.AddTeamProject` and `core.RemoveTeamProject` mutate the
parsed configuration, `core.WriteTeamConfig` puts it back through the single emitter, and the
two vault methods `team.project.add` and `team.project.remove` expose them to every surface —
REST, the WASM bridge and all three web providers. A settings card lists the active team's
projects, offers the locally registered repositories as candidates with their key, name, docs
folder, remote URL and branch already filled in, and removes an entry.

The link between a registered repository and a `team.yaml` entry is **the project key alone**,
never a path and never a remote URL (doc 04 §7.1). That is what the card makes visible: an
entry is `cloned` when some open repository serves that key, and it renders from a snapshot
otherwise. It is also the failure mode this UI produces, so a clone whose `project.yaml`
declares a different key is surfaced as `W-TEAM-KEY-MISMATCH` on the entry itself.

Removing a project is not allowed to silently break the boards and sprints that reference its
items. A removal that would orphan a `ref:` is refused with `team_project_referenced`, listing
what points where; the caller may repeat it with `force` and is told exactly what it is
accepting. See doc 04 §3.9.

## Acceptance Criteria

- [x] `internal/core` adds and removes a `team.yaml` project entry, validating the entry and
      refusing a duplicate key; the file goes back through `core.MarshalTeamConfig`, the one
      emitter, so the round trip stays byte-stable.
- [x] A new entry is inserted in key order rather than appended, and a removal touches only
      its own lines, so two people connecting different projects produce a mergeable diff.
- [x] `team.project.add` and `team.project.remove` are team-scoped vault methods, resolved
      through `Workspace.TeamMount` like every other team-scoped call (R-TEAM-ACT-1).
- [x] A duplicate project key is refused with `team_project_exists` and a message naming the
      key and the team.
- [x] Removing a project that boards or sprints reference is refused with
      `team_project_referenced` and the list of references; `force` accepts the breakage.
- [x] `POST /api/v1/teams/{key}/projects` and `DELETE /api/v1/teams/{key}/projects/{project}`
      serve both, and both are typed in the WASM bridge contract and implemented by the
      browser, companion and fake providers.
- [x] Settings holds a "Team projects" card scoped to the active team of GIT-US-0036, with no
      second team-selection mechanism.
- [x] Adding a project offers the locally registered repositories as candidates and pre-fills
      key, name, docs folder, remote URL and default branch from what is already indexed.
- [x] Every entry says whether it is cloned or renders from a snapshot, and a clone declaring
      a different key shows `W-TEAM-KEY-MISMATCH`.
- [x] Table-driven Go tests cover both operating modes; Vitest covers the candidate list, the
      duplicate refusal and the referenced-project refusal.
- [x] Docs 03, 04, 05 and 07 updated.

## Notes

No ADR. Refusing a destructive write while a reference points at it, and taking `force` to
override, is the rule board deletion already follows (R-BOARD-DEL-1, `sprint_active`) and the
one a WIP-limited move follows; this story applies it, it does not decide it.

`local_hints` are deliberately not written by the UI. They are per-machine and usually wrong
for everybody else (R-PROJ-3), and the user's own settings file already overrides them.
