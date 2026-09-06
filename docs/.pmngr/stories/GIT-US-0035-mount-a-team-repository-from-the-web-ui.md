---
id: GIT-US-0035
type: story
title: Mount a team repository from the web UI
status: done
priority: high
parent: GIT-EP-0009
milestone: GIT-M-0009
author: team
labels: [web, server]
estimate: 5
created: 2026-09-05
updated: 2026-09-06T00:00:00Z
closed: 2026-09-06T00:00:00Z
---

## Description

As someone whose team already has a team repository, I want to mount it from the web app, so
that boards, sprints and retrospectives appear without dropping into a terminal.

The add-repository wizard hardcodes `kind: 'project'`, so the only role the web app can ever
register is "project", even though the provider contract, the browser provider and the vault
have accepted `'team'` since Phase 3. A folder holding a `team.yaml` is therefore mounted as a
project, and the workspace never sees a team.

The empty states make the gap worse by promising what the UI cannot do. The board index says
"Mount a team repository with `.pmngr/boards/`…" while offering no way to mount one; "New
board" is disabled with no explanation at all; "Start a retro" is enabled with no team guard
and fails with a raw `not_found` from the workspace.

In companion mode `POST /api/v1/repos` answers 501 because registering a repository is a
change to the user's configuration file, which belongs to the CLI. That stays true here: the
route reports the exact `gintrack add` command to run rather than pretending to succeed
(ADR-020).

## Acceptance Criteria

- [x] The wizard detects a root `team.yaml` in the picked folder and reads its key and name.
- [x] The user chooses the role deliberately; the wizard registers a team repository with
      `kind: 'team'` when they do.
- [x] A folder with no `team.yaml` and no `project.yaml` offers both "create a project" and
      "create a team repository" instead of a dead end.
- [x] `POST /api/v1/repos` still answers 501, now with the exact `gintrack add` command for
      the path and role that were requested; nothing fakes a successful registration.
- [x] The companion-mode wizard surfaces that command instead of a bare failure.
- [x] The board empty state no longer asks the user to do something the UI cannot do, and
      "New board" explains why it is disabled.
- [x] "Start a retro" is disabled with an explanation when no team repository is open, so it
      can no longer surface a raw `not_found`.
- [x] docs/05 and docs/07 describe the wizard and the 501 contract.
- [x] `make lint`, `make test`, `make wasm`, `make wasm-smoke` and `make build` pass.

## Notes

Mounting is not registering: in browser-only mode the mount record in IndexedDB is the whole
registration, while in companion mode the registration lives in the user's configuration file
and only the CLI writes it.
