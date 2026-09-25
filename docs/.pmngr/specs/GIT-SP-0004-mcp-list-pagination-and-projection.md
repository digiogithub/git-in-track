---
id: GIT-SP-0004
type: spec
title: MCP list pagination and projection
status: in_review
labels: [mcp]
created: 2026-09-24T22:59:01Z
updated: 2026-09-24T22:59:37Z
started: 2026-09-24T22:59:01Z
requirements:
  R1:
    status: in_review
  R2:
    status: in_review
  R3:
    status: in_review
  R4:
    status: in_review
  R5:
    status: in_review
  R6:
    status: in_review
  R7:
    status: in_review
---

## Purpose

Keep every MCP list answer inside a small token budget and make it safe to walk: pages are
bounded, continuation is explicit, a cursor cannot be replayed against another query, and a
projection returns only the fields an agent asked for plus what it needs to read or write the
entry again.

## Scope

Covers `list_items`, `search_items`, `search_kb`, `list_kb_pages`, `list_requirements` and
`spec_coverage` (docs/08 §3.4), and the `fields` projection of items and requirements. The core's
own item query (`item.list`) is in scope where `list_items` passes its cursor through.

## Glossary

- **walk** — a sequence of calls, each passing back the previous `nextCursor` with every other argument unchanged.
- **filter fingerprint** — a short hash of the tool name and the filter arguments that a cursor carries.

## Requirements

### GIT-SP-0004.R1 — Bounded page size

WHEN a list tool is called, the MCP server SHALL use a page of 20 entries for a missing, zero or negative `limit` and cap any larger `limit` at 100.

#### Scenario: an oversized limit
- **WHEN** `list_items` is called with `limit: 10000`
- **THEN** the page holds at most 100 items

#### Scenario: no limit
- **WHEN** a list tool is called without `limit`
- **THEN** the page size is 20

### GIT-SP-0004.R2 — A cursor only for a partial page

The MCP server SHALL return a `nextCursor` when a page ends before the last matching entry, and none on the page that ends the list, so a walk stops without an extra empty call.

#### Scenario: a two-item page of a longer list
- **WHEN** `list_items` is called with `limit: 2` over more than two items
- **THEN** the result carries a `nextCursor`

#### Scenario: the last page
- **WHEN** the page reaches the last matching entry
- **THEN** the result carries no `nextCursor`

### GIT-SP-0004.R3 — A walk returns every entry once

WHEN a client passes each `nextCursor` back with every other argument unchanged, the walk SHALL return every matching entry exactly once and then end.

#### Scenario: walking requirements two at a time
- **GIVEN** a spec holds `R1`, `R2` and `R3`
- **WHEN** `list_requirements` is walked with `limit: 2`
- **THEN** the rows are `R1`, `R2`, `R3` in that order, each once

#### Scenario: walking knowledge-base pages
- **WHEN** `list_kb_pages` is walked with `limit: 1`
- **THEN** the second page is not the first page again

### GIT-SP-0004.R4 — Cursors are bound to their filter

IF a cursor of `list_items`, `list_inbox`, `search_items`, `search_kb`, `list_kb_pages`, `list_requirements` or `spec_coverage` is presented with a different query, project or filter than the call that issued it, THEN the MCP server SHALL refuse the call with `invalid_cursor` on `cursor`.

#### Scenario: a filter changed mid-walk
- **GIVEN** a `list_requirements` page for project GIT with `limit: 1` returned a `nextCursor`
- **WHEN** the cursor is passed back with `status: [todo]` added
- **THEN** the call is refused with `invalid_cursor`

### GIT-SP-0004.R5 — The list_items cursor is bound to its sort

IF a `list_items` cursor is presented with a different sort than the call that issued it, THEN the core SHALL refuse the cursor instead of returning a page of another ordering.

#### Scenario: the sort changed mid-walk
- **GIVEN** a page sorted by `id` returned a `nextCursor`
- **WHEN** the cursor is passed back with the sort `-updated`
- **THEN** the query fails

### GIT-SP-0004.R6 — A projection keeps the identity and the rev

WHEN a read tool is given a `fields` projection, the MCP server SHALL return only the named fields plus `id` and `rev` of an item, or `ref` and `rev` of a requirement, ignoring unknown field names.

#### Scenario: title and status only
- **WHEN** `list_items` is called with `fields: [title, status]`
- **THEN** every item carries `id`, `rev`, `title` and `status`
- **AND** no item carries `priority` or `labels`

#### Scenario: requirement text on request
- **WHEN** `list_requirements` is called with `fields: [text, blockRev]`
- **THEN** every row carries `ref`, `rev`, `text` and `blockRev` and no `title`

### GIT-SP-0004.R7 — Lists never return bodies

The `list_items` tool SHALL NOT return an item body, whatever the `fields` projection names.

#### Scenario: body asked for in a list
- **WHEN** `list_items` is called with `fields: [title, body]`
- **THEN** no item of the page carries a body

## Notes

Dogfood spec of GIT-US-0136. `list_items` delegates paging to the core, whose cursor is bound to
the sort and resumes after the last item returned; since GIT-US-0155 the MCP layer also binds
that cursor to every filter, so a filter changed mid-walk is refused (R4).
