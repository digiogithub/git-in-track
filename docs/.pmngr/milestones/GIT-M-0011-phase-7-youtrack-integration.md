---
id: GIT-M-0011
type: milestone
title: Phase 7 — YouTrack integration
status: done
author: mcp
labels: [server, web, core, docs]
created: 2026-09-13T13:06:45Z
updated: 2026-09-13T19:53:29Z
started: 2026-09-13T19:53:19Z
closed: 2026-09-13T19:53:29Z
due: 2026-11-15
---

## Description

Connect a git-in-track project to a JetBrains YouTrack project and move work between them: link with a permanent token, import epics, user stories, tasks and versions (with subtasks, links and comments) into the local backlog, push local comments and feedback notes back to the linked issue, and sync knowledge base pages with YouTrack articles. All of it runs through a new background sync engine (queue, workers, batches, retries) configured from Settings.

Companion mode only: browser-only mode cannot reach YouTrack (the CORS proxy is a git proxy, ADR-025) and degrades with a notice.

## Acceptance Criteria

- [ ] A project can be linked to a YouTrack project from Settings with an autosuggest picker; the token never lands in the repository.
- [ ] Backlog offers "Import from YouTrack" with search + autosuggest; imported items carry a first-class `external` reference to the YouTrack issue, plus subtasks, links and comments.
- [ ] Comments and feedback notes on a linked item can be pushed to YouTrack, manually or automatically.
- [ ] KB pages can be published to and synced with YouTrack articles.
- [ ] The sync engine runs jobs in batches with configurable workers and shows progress over WebSocket; its settings live in the Settings UI.
- [ ] `docs/03-data-model.md`, `docs/07-cli-and-api.md` and new ADRs (external references, stored integration credentials) are updated in the same changes.

## Notes

Due date is a planning estimate. Analysis sources: youtrack-cli (`/www/youtrack-cli`) for the REST surface, Plane importers for the `(external_source, external_id)` idempotency contract and job states.
