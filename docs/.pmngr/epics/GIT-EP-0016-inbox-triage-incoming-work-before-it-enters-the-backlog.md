---
id: GIT-EP-0016
type: epic
title: "Inbox: triage incoming work before it enters the backlog"
status: done
priority: medium
milestone: GIT-M-0012
author: mcp
labels: [core, server, web, mcp]
created: 2026-09-13T13:08:07Z
updated: 2026-09-15T21:45:46Z
started: 2026-09-15T21:45:41Z
closed: 2026-09-15T21:45:46Z
---

## Description

Modelled on Plane's Intake. Anything can drop work into a project's inbox — a person from the UI, an agent over MCP, a YouTrack import set to "land in inbox", the REST API — and it waits there in a triage state until someone accepts it (it becomes a normal backlog item, with the edit form open to pick type, parent and status), rejects it, snoozes it until a date, or marks it a duplicate of an existing item. Inbox items are real items on disk so they get ids, history and comments, but the index keeps them out of the backlog, boards and metrics.

## Acceptance Criteria

- [ ] Data model: an `inbox` front-matter block (`status: pending|accepted|rejected|snoozed|duplicate`, `snoozed_until`, `duplicate_of`, `source`) on items; workflow gains a reserved `triage` status category; documented with an ADR.
- [ ] Index excludes triage items from backlog, boards, sprints and metrics; a dedicated query lists them.
- [ ] Inbox route in the web app with list, filters (pending, snoozed, all), detail pane and next/previous navigation; accept opens the item form; reject/snooze/duplicate actions with confirmation.
- [ ] REST and MCP: `create_inbox_item`, `list_inbox`, `triage_inbox_item`; CLI `gintrack inbox list|accept|reject`.
- [ ] YouTrack import option "send to inbox".
- [ ] Snooze expiry is a query filter (`snoozed_until <= now` shows as pending), no scheduler.

## Notes

Plane reference: `apps/api/plane/db/models/intake.py`, `web/core/components/intake/`. Accept reuses the edit form rather than moving state server-side.
