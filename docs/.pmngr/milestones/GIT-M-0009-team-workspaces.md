---
id: GIT-M-0009
type: milestone
title: Team workspaces
status: in_progress
priority: high
author: team
created: 2026-09-05T00:00:00Z
updated: 2026-09-05T00:00:00Z
---

## Description

Post-1.0 milestone for `GIT-EP-0009`: a user can create and mount team
repositories from the app, keep several of them, choose which one they are
working in, and manage that team's projects without editing YAML.

## Exit criteria

- [ ] A team repository can be created from the web UI and from the CLI, in a
      folder the user chooses, including one that has no `team.yaml` yet.
- [ ] A team repository can be mounted from the web UI, and mounting a folder
      that is not one is refused with an actionable message.
- [ ] Several team repositories can be mounted, and the active one is chosen in
      the UI; boards, sprints, retros and the team knowledge base follow it.
- [ ] A team's projects are added and removed from settings, writing
      `team.yaml`, with the cloned/remote state visible.
- [ ] No empty state tells the user to do something the UI cannot do.
