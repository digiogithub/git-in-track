---
id: GIT-US-0033
type: story
title: Reach every create flow from where the user is
status: in_review
created: 2026-09-05
updated: 2026-09-05
author: team
priority: medium
parent: GIT-EP-0008
milestone: GIT-M-0008
estimate: 5
labels: [web, mcp]
---

## Description

As a user, we want to create an item from the view we are already looking at, so that
authoring a backlog does not force a detour through the items table.

The write path is not the gap. The web app can already create all four item types from
`NewItemPage` (`/p/$project/items/new`), and the draft travels through
`provider.createItem` → `item.create` → the shared core, which allocates the id and
validates the draft. What is missing is *reach*: the only entry point is the items table,
so the epic tree, the milestone list and an item's own detail page are dead ends. Creating a
story under the epic on screen means going back to the table and retyping the parent id.

The same gap exists on the agent surface: MCP ships `create_epic`, `create_story` and
`create_task`, so a milestone is the one item type an agent cannot create even though the
core method behind it is the same `item.create`.

This story is surfaces only. No new validation, no second create implementation: every new
control routes into the existing `NewItemPage` with `type`, `parent` and `milestone`
pre-filled through the search parameters the route already validates, and
`create_milestone` is one more registration over the existing shared create handler.

## Acceptance Criteria

- [x] The epic tree offers a create control that opens the new-item page for an epic.
- [x] From an epic in the tree, a story can be created with `parent` already pre-filled.
- [x] From a story in the tree, a task can be created with `parent` already pre-filled.
- [x] The milestone list offers a create control for a milestone, and each milestone offers
      one that pre-fills `milestone` on the new item.
- [x] An item's detail page offers a create control for its children — a story from an
      epic, a task from a story — with `parent` pre-filled, and the saved child shows up in
      the parent's children list.
- [x] Every new control reuses `NewItemPage` and its provider call; there is no second
      create implementation.
- [x] MCP exposes `create_milestone` with the shape and schema conventions of the three
      existing create tools, over stdio and over `POST /mcp`, going through `item.create`.
- [x] The tool catalog in `docs/08-mcp-server.md`, the handshake instructions, `AGENTS.md`
      and the `--list-tools` expectations name thirteen tools.
- [x] `make lint`, `make test`, `make wasm`, `make wasm-smoke` and `make build` pass.

## Notes

Audited before implementing: `NewItemPage` already supports all four types
(`templates.ts`), and `validateNewItemSearch` already reads `type`, `parent` and
`milestone` from the query string, so pre-filling needs no new route and no new schema.
`ItemDetail` already renders the children of an epic or a story; the create control is added
next to that list so the relationship is visible immediately after saving.

`create_milestone` reuses `CreateItemInput` and the shared `createTool` handler; the core
already refuses a `parent` on a milestone (`internal/core/validate.go`, `parentCodes`), so
no per-type check belongs in `internal/mcp`.

Gates were run against a working tree that also carries the in-flight changes of
GIT-US-0031 and GIT-US-0032. Everything this story touches is green; the failures left in
the tree belong to those stories: `gofmt` on `cmd/gintrack/add.go` and
`internal/core/boardview.go`, ESLint on `web/src/features/boards/board-form.ts`, and
`internal/server` `TestStartAndShutdown`, which cannot bind 127.0.0.1:7317 while a local
`gintrack serve` holds it.
