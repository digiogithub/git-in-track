---
id: GIT-EP-0009
type: epic
title: Team workspaces
status: done
priority: high
milestone: GIT-M-0009
author: team
labels: [core, web, cli]
estimate: 21
created: 2026-09-05T00:00:00Z
updated: 2026-09-06T00:00:00Z
closed: 2026-09-06T00:00:00Z
---

## Description

Post-1.0. A team repository is the home of boards, sprints and retrospectives,
but nothing in the product can create one, mount one from the web, hold more
than one, or connect projects to one. An audit of the shipped code found:

- `gintrack add --team` is the only way to mount a team repository, and the role
  it sets is inert: a folder with no `team.yaml` registers happily, shows as
  "Team repository" in the UI, and every team surface still fails.
- Nothing writes a `team.yaml`. There is `core.CreateProject` and no
  `core.CreateTeam`, no vault method, no route, no CLI verb, no UI.
- A workspace holds one team, first one wins; a second team repository is
  registered and then ignored with an error-severity diagnostic. The web app has
  no notion of an active team — `getTeam()` takes no id.
- The project list inside `team.yaml` is hand-edited only. A registered
  repository is linked to a team entry by project key alone, which is what
  decides whether a card renders live or from a snapshot.
- The empty states tell the user to "mount a team repository with `.pmngr/`"
  while offering no way to do it; "New board" is disabled with no explanation
  and "Start a retro" is enabled and fails with a raw `not_found`.

## Notes

Boards, sprints and retros living in a separate team repository is the design
(ADR-007), not an accident. This epic makes that design reachable, it does not
move team artifacts into project repositories.
