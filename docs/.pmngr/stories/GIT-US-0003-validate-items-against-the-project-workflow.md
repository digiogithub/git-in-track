---
id: GIT-US-0003
type: story
title: Validate items against the project workflow
status: backlog
created: 2026-09-03T00:00:00Z
updated: 2026-09-03T00:00:00Z
author: team
priority: high
parent: GIT-EP-0001
milestone: GIT-M-0001
estimate: 3
labels: [core, cli]
links:
  - kind: blocked_by
    target: GIT-US-0002
---

## Description

As a user, I want to be told exactly what is wrong with an item file, so that a typo in a
status or a dangling parent reference does not quietly corrupt my backlog.

Validation checks the item against the schema and against the workflow configured in
`project.yaml`: the status must exist in the active workflow, the priority must be one of
the configured values, `parent` must reference an existing item of the right type
(story → epic, task → story), `milestone` must exist, dates must be valid, and `links`
targets must resolve.

All problems in a file are collected before returning, joined with `errors.Join`, so the UI
can show every issue at once. `gintrack validate <path>` exposes this from the CLI and is
what CI runs against this repository's own `docs/.pmngr/`.

## Acceptance Criteria

- [ ] Unknown status, unknown priority and unknown type are reported with field and value.
- [ ] A `parent` of the wrong type, or a missing target, is reported as a distinct error.
- [ ] Multiple problems in one file are all reported in a single pass.
- [ ] Errors carry the file path and the front-matter field name (`*ValidationError`).
- [ ] Transitions not allowed by the workflow are rejected on write, not on read.
- [ ] `gintrack validate docs/.pmngr` exits non-zero on the `invalid` fixtures and zero on
      `project-basic`.
- [ ] Table-driven tests cover every rule, including the valid cases.
