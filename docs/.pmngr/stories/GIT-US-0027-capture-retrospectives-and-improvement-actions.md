---
id: GIT-US-0027
type: story
title: Capture retrospectives and improvement actions
status: backlog
created: 2026-09-03T00:00:00Z
updated: 2026-09-03T00:00:00Z
author: team
priority: high
parent: GIT-EP-0007
milestone: GIT-M-0007
estimate: 5
labels: [web, core]
links:
  - kind: blocked_by
    target: GIT-US-0018
---

## Description

As a team, we want to run our retrospectives in the tool and keep them in git, so that our
improvement actions live next to the work instead of in a slide deck nobody reopens.

A retro is `.pmngr/retros/<slug>.md` with `sprint`, `date`, `participants[]` and the
sections `## Went well`, `## To improve`, `## Actions` with checkboxes. Selected
improvements are recorded in `actions[]` in the front matter, and any action can be promoted
into a real task in a project repository, with a link back to the retro so the origin of the
work is never lost.

Previous retros and the state of their actions are visible when starting a new one, because
the point is following through, not writing notes.

## Acceptance Criteria

- [ ] A retro can be created for a sprint with participants and the three sections.
- [ ] Items can be added, edited and grouped during the session.
- [ ] Selected improvements are written to `actions[]` in the front matter.
- [ ] An action can be promoted to a task in a chosen project repository.
- [ ] The created task links back to the retro and the retro links to the task.
- [ ] Open actions from previous retros are shown when a new retro starts.
- [ ] Retro files stay valid Markdown that reads well in a plain editor and on the host.
- [ ] Concurrent editing by several participants merges without losing entries.
