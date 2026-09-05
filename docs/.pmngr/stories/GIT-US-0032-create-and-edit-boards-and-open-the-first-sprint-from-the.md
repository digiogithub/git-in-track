---
id: GIT-US-0032
type: story
title: Create and edit boards, and open the first sprint, from the web UI
status: in_review
priority: high
parent: GIT-EP-0008
milestone: GIT-M-0008
author: claude
labels: [core, web]
estimate: 8
created: 2026-09-05T00:00:00Z
updated: 2026-09-05T00:00:00Z
---

## Description

A team that has just opened a team repository cannot get a board without
hand-writing `.pmngr/boards/<slug>.md`. The core can already write a board file
(`BoardStore.Write`), but nothing reaches it: there is no `board.create` in the
vault dispatcher, no POST route and no control in the board index. Editing is
in the same shape from the user's side: `board.update` and `updateBoard` exist
on all three providers, but no hook and no UI ever call them.

The consequence the product owner hit is "I cannot put my epics, stories or
tasks on a board". Cards are a derived query — a board shows what its project
scope and its filters select, never a copy of an item — so the fix is to make
scope and filter editing a first-class, well-explained flow rather than to
invent a way of pasting items into a board file.

The second dead end is the first sprint of a scrum board. `SprintPanel` renders
only once the board already points at a sprint, and there is no sprint list
route, so a new scrum board tells the user to "point it at one from the sprint
list" that does not exist.

## Acceptance Criteria

- [x] A board can be created from the web UI, kanban or scrum: title, kind,
      projects in scope and a default, editable column set.
- [x] Board creation is domain logic in `internal/core` — slug allocation, the
      default columns, validation — and refuses to overwrite an existing board.
- [x] `board.create` is reachable from the vault dispatcher, from
      `POST /api/v1/boards` and from all three web providers.
- [x] A board can be edited from the web UI: title, description, project
      scope, columns with their mapped statuses and WIP limits, filters and
      the scrum backlog column.
- [x] The scope and filter editor explains that cards are a derived query, so
      that putting epics, stories or tasks on a board is an obvious act.
- [x] A board can be deleted from the web UI behind a confirmation, and a
      board an active sprint points at is refused.
- [x] A brand-new scrum board can get its first sprint from the UI, without
      hand-editing a file.
- [x] A board file written by the UI round-trips byte-identically through the
      existing emitter, and two boards created independently produce a
      mergeable diff.
- [x] Both operating modes and the fake provider keep parity.
- [x] `make lint`, `make test`, `make wasm`, `make wasm-smoke` and `make build`
      pass, and docs 03, 04, 05 and 07 are updated.

## Notes

Cards on a board are derived (`internal/core/boardview.go`), so "add an item to
a board" means widening the board's project scope or relaxing its filters, or
changing the item's own fields. The board editor says so in the UI rather than
leaving the user to infer it.

The default columns map **status categories**, not status ids (docs/04 R-COL-2):
a categories column works for a project whose workflow this team has never seen,
which is exactly the situation of a board created before the projects are
cloned.
