---
id: GIT-EP-0008
type: epic
title: Authoring a workspace from the web UI
status: in_progress
priority: high
milestone: GIT-M-0008
author: team
labels: [core, web, cli]
estimate: 21
created: 2026-09-05T00:00:00Z
updated: 2026-09-05T00:00:00Z
---

## Description

Post-1.0. Everything a team needs to start from an empty repository and run the
process without hand-writing a single file: create the project itself, create
boards, open the first sprint, and reach every create flow from the UI rather
than only from the items table.

The gaps this epic closes were found by auditing the shipped product against
what a new user can actually do:

- There is no project-creation path anywhere. `gintrack add` on a repository
  with no `.pmngr/` warns and mounts it empty; the web equivalent offers "mount
  it anyway". Nothing ever writes a `project.yaml`.
- Project discovery walks the whole working tree, so unrelated `.pmngr/` trees
  (test fixtures, vendored samples) surface as real projects.
- Boards cannot be created from any surface: core can write the file, but no
  vault method, no route, no MCP tool and no UI reach it.
- A scrum board with no sprint yet is a dead end: the sprint panel only renders
  once the board already points at one, and there is no sprint list route.
- Epic and milestone views have no create control, so the only way in is the
  items table.
- MCP has `create_epic`, `create_story` and `create_task` but no
  `create_milestone`.

## Notes

Retrospectives and item creation (all four types, milestones included) already
work from the web UI and are out of scope; see the audit recorded on the
stories below.
