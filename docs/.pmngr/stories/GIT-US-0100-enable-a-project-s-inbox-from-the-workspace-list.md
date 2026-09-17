---
id: GIT-US-0100
type: story
title: Enable a project's inbox from the workspace list
status: in_review
priority: high
parent: GIT-EP-0021
milestone: GIT-M-0014
author: mcp
labels: [core, server, web]
estimate: 5
created: 2026-09-17T09:31:52Z
updated: 2026-09-17T09:47:01Z
started: 2026-09-17T09:33:16Z
---

## Description

As a user with projects created before the inbox existed, I want an "Enable inbox" button on each such project in the workspace list, so that I do not have to edit `project.yaml` by hand.

A project has an inbox when its workflow declares a status of category `triage` (ADR-033). The action adds `{id: triage, name: Triage, category: triage}` as the first status, leaves `initial` and `transitions` untouched (triage is never a transition target), and writes through the vault so the write set, commit-on-save and the browser host behave like any other write.

## Acceptance Criteria

- [ ] A core/vault method (e.g. `project.inbox.enable`) adds the triage status to one project's `project.yaml`, rev-guarded, preserving the rest of the file (comments and key order as far as the YAML layer allows) and refusing a project that already has one.
- [ ] An id clash (a non-triage status already called `triage`) is refused with a clear problem code.
- [ ] Companion route and both providers (companion, browser/wasm) plus the fake provider expose it.
- [ ] The workspace project list shows the button only for writable projects without a triage status; after success the inbox nav link appears without a reload.
- [ ] Go, server and component tests cover success, already-enabled and read-only.
- [ ] docs/07 (API) and docs/05 (web app) updated.
